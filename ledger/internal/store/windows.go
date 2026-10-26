package store

const journalPredicates = ` AND (?3 = '' OR EXISTS (SELECT 1 FROM postings p WHERE p.txn=t.seq AND (p.account=?3 OR (p.account>?3||':' AND p.account<?3||';')))) AND (?4 = '' OR EXISTS (SELECT 1 FROM tags g WHERE g.tag=?4 AND g.txn=t.seq)) AND (?5 = '' OR EXISTS (SELECT 1 FROM links l WHERE l.link=?5 AND l.txn=t.seq)) AND EXISTS (SELECT 1 FROM annotations a WHERE a.txn=t.seq AND a.version=t.version AND (?6='' OR a.payee=?6) AND (?7='' OR a.flag=?7))`

// TransactionsFirst selects the first journal window.
const TransactionsFirst = `SELECT t.seq FROM transactions t INDEXED BY transactions_journal WHERE t.date>=?1 AND t.date<=iif(?2='', '9999-12-31', ?2)` + journalPredicates + ` ORDER BY t.date DESC,t.seq DESC LIMIT ?8`

// TransactionsAfter selects the journal window after a date and sequence.
const TransactionsAfter = `SELECT t.seq FROM transactions t INDEXED BY transactions_journal WHERE t.date>=?1 AND (t.date,t.seq)<(?8,?9) AND t.date<=iif(?2='', '9999-12-31', ?2)` + journalPredicates + ` ORDER BY t.date DESC,t.seq DESC LIMIT ?10`

// RegisterFirst selects the first register window.
const RegisterFirst = `SELECT p.txn,p.position FROM postings p INDEXED BY postings_register WHERE p.date>=?2 AND p.date<=iif(?3='', '9999-12-31', ?3) AND (p.account=?1 OR (p.account>?1||':' AND p.account<?1||';')) ORDER BY p.date,p.txn,p.position LIMIT ?4`

// RegisterAfter selects the register window after a posting position.
const RegisterAfter = `SELECT p.txn,p.position FROM postings p INDEXED BY postings_register WHERE (p.date,p.txn,p.position)>(?4,?5,?6) AND p.date>=?2 AND p.date<=iif(?3='', '9999-12-31', ?3) AND (p.account=?1 OR (p.account>?1||':' AND p.account<?1||';')) ORDER BY p.date,p.txn,p.position LIMIT ?7`

// VersionsFirst selects the first annotation history window.
const VersionsFirst = `SELECT a.version FROM annotations a WHERE a.txn=?1 ORDER BY a.version DESC LIMIT ?2`

// VersionsAfter selects annotation versions below a position.
const VersionsAfter = `SELECT a.version FROM annotations a WHERE a.txn=?1 AND a.version<?2 ORDER BY a.version DESC LIMIT ?3`
const failingPredicate = ` AND a.amount <> (SELECT COALESCE(SUM(p.amount),0) FROM postings p WHERE (p.account=a.account OR (p.account>a.account||':' AND p.account<a.account||';')) AND p.date<=a.date)`

// FailingFirst selects the first failing assertion window.
const FailingFirst = `SELECT a.id FROM assertions a INDEXED BY assertions_check WHERE a.retracted='' AND a.date<=?1` + failingPredicate + ` ORDER BY a.date,a.account,a.id LIMIT ?2`

// FailingAfter selects failing assertions after a check position.
const FailingAfter = `SELECT a.id FROM assertions a INDEXED BY assertions_check WHERE a.retracted='' AND (a.date,a.account,a.id)>(?2,?3,?4) AND a.date<=?1` + failingPredicate + ` ORDER BY a.date,a.account,a.id LIMIT ?5`
