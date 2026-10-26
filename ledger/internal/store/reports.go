package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"
)

// reportBalances rolls each declared account's dated postings through its parents.
func reportBalances(ctx context.Context, tx *sql.Tx, from, to string) (out map[string]int64, accounts map[string]bool, resultErr error) {
	declared := map[string]bool{}
	balances := map[string]int64{}
	rows, err := tx.QueryContext(ctx, `SELECT name FROM accounts ORDER BY name`)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, nil, errors.Join(err, rows.Close())
		}
		declared[name] = true
		for _, parent := range parents(name) {
			balances[parent] = 0
		}
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return nil, nil, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT account,SUM(amount) FROM postings WHERE date>=? AND date<=? GROUP BY account`, from, to)
	if err != nil {
		return nil, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	for rows.Next() {
		var name string
		var units int64
		if err = rows.Scan(&name, &units); err != nil {
			return nil, nil, err
		}
		for _, parent := range parents(name) {
			balances[parent] += units
		}
	}
	return balances, declared, rows.Err()
}
func parents(name string) []string {
	out := []string{name}
	for {
		i := strings.LastIndexByte(name, ':')
		if i < 0 {
			return out
		}
		name = name[:i]
		out = append(out, name)
	}
}
func reportLines(ctx context.Context, tx *sql.Tx, balances map[string]int64, declared map[string]bool, include func(string) bool) ([]Line, error) {
	names := []string{}
	for name, units := range balances {
		if include(name) && (units != 0 || declared[name]) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	out := make([]Line, 0, len(names))
	for _, name := range names {
		a, err := amount(ctx, tx, balances[name])
		if err != nil {
			return nil, err
		}
		out = append(out, Line{Account: name, Amount: a})
	}
	return out, nil
}

// Balances returns dated account balances rolled through their parents.
func (s *Store) Balances(ctx context.Context, date, prefix string) (Trial, error) {
	var out Trial
	err := s.do(ctx, false, func(tx *sql.Tx, stamp time.Time) error {
		if (date != "" && !ValidDate(date)) || (prefix != "" && !accountPrefix(prefix)) {
			return ErrInvalid
		}
		if date == "" {
			date = stamp.Format(DateLayout)
		}
		balances, declared, err := reportBalances(ctx, tx, "", date)
		if err != nil {
			return err
		}
		out.Date = date
		out.Lines, err = reportLines(ctx, tx, balances, declared, func(name string) bool { return prefix == "" || subtree(name, prefix) })
		if err != nil {
			return err
		}
		var total int64
		if prefix != "" {
			total = balances[prefix]
		} else {
			for _, root := range []string{RootAssets, RootLiabilities, RootEquity, RootIncome, RootExpenses} {
				total += balances[root]
			}
		}
		out.Total, err = amount(ctx, tx, total)
		return err
	})
	if err != nil {
		return Trial{}, err
	}
	return out, nil
}

// BalanceSheetAt returns assets, liabilities and equity with fiscal earnings.
func (s *Store) BalanceSheetAt(ctx context.Context, date string) (BalanceSheet, error) {
	var out BalanceSheet
	err := s.do(ctx, false, func(tx *sql.Tx, stamp time.Time) error {
		if date != "" && !ValidDate(date) {
			return ErrInvalid
		}
		if date == "" {
			date = stamp.Format(DateLayout)
		}
		b, err := book(ctx, tx)
		if err != nil {
			return err
		}
		start := date[:4] + "-" + b.FiscalYearStart
		if start > date {
			year, err := time.Parse(DateLayout, date)
			if err != nil {
				return err
			}
			start = year.AddDate(-1, 0, 0).Format("2006") + "-" + b.FiscalYearStart
		}
		balances, declared, err := reportBalances(ctx, tx, "", date)
		if err != nil {
			return err
		}
		current, _, err := reportBalances(ctx, tx, start, date)
		if err != nil {
			return err
		}
		earnings := current[RootIncome] + current[RootExpenses]
		previous := balances[RootIncome] + balances[RootExpenses] - earnings
		for _, name := range parents(EarningsCurrent) {
			balances[name] += earnings
		}
		for _, name := range parents(EarningsPrevious) {
			balances[name] += previous
		}
		out.Date = date
		out.Lines, err = reportLines(ctx, tx, balances, declared, func(name string) bool {
			return subtree(name, RootAssets) || subtree(name, RootLiabilities) || subtree(name, RootEquity)
		})
		return err
	})
	if err != nil {
		return BalanceSheet{}, err
	}
	return out, nil
}

// IncomeStatementFor returns income and expenses over an inclusive date range.
func (s *Store) IncomeStatementFor(ctx context.Context, from, to string) (IncomeStatement, error) {
	var out IncomeStatement
	err := s.do(ctx, false, func(tx *sql.Tx, _ time.Time) error {
		if !ValidDate(from) || !ValidDate(to) || from > to {
			return ErrInvalid
		}
		balances, declared, err := reportBalances(ctx, tx, from, to)
		if err != nil {
			return err
		}
		out.From = from
		out.To = to
		out.Lines, err = reportLines(ctx, tx, balances, declared, func(name string) bool { return subtree(name, RootIncome) || subtree(name, RootExpenses) })
		if err != nil {
			return err
		}
		out.Net, err = amount(ctx, tx, -balances[RootIncome]-balances[RootExpenses])
		return err
	})
	if err != nil {
		return IncomeStatement{}, err
	}
	return out, nil
}
