package spaceref

import (
	"fmt"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/appref"
)

// Space identifies a space by its label and full domain name.
type Space struct {
	Label  string
	Domain string
}

// App identifies an application on a space.
type App struct {
	Name     string
	Space    Space
	Hostname string
}

// NotASpaceError reports an operand that cannot name one label under the root.
type NotASpaceError struct {
	Operand string
	Root    string
}

func (e *NotASpaceError) Error() string {
	return fmt.Sprintf("'%s' is not a space: a space is one label under '%s'", e.Operand, e.Root)
}

// ExitCode reports a command-line usage failure.
func (e *NotASpaceError) ExitCode() int { return 2 }

// InvalidLabelError reports an operand containing an invalid DNS label.
type InvalidLabelError struct {
	Operand string
}

func (e *InvalidLabelError) Error() string {
	return fmt.Sprintf("'%s' is not a valid label", e.Operand)
}

// ExitCode reports a command-line usage failure.
func (e *InvalidLabelError) ExitCode() int { return 2 }

// NotAnAppError reports an operand that cannot name an app on a space.
type NotAnAppError struct {
	Operand string
}

func (e *NotAnAppError) Error() string {
	return fmt.Sprintf("'%s' is not an app on a space: <app>.<space>", e.Operand)
}

// ExitCode reports a command-line usage failure.
func (e *NotAnAppError) ExitCode() int { return 2 }

// UnusableAppError reports a valid label reserved from use as an app name.
type UnusableAppError struct {
	Name string
}

func (e *UnusableAppError) Error() string {
	return fmt.Sprintf("'%s' is not a usable app name", e.Name)
}

// ExitCode reports a command-line usage failure.
func (e *UnusableAppError) ExitCode() int { return 2 }

// ValidLabel reports whether label is a lowercase RFC 1123 label.
func ValidLabel(label string) bool {
	if len(label) == 0 || len(label) > 63 || !asciiLowerOrDigit(label[0]) || !asciiLowerOrDigit(label[len(label)-1]) {
		return false
	}
	for i := 1; i < len(label)-1; i++ {
		if !asciiLowerOrDigit(label[i]) && label[i] != '-' {
			return false
		}
	}
	return true
}

// Parse parses an abbreviated or fully qualified space operand.
func Parse(operand, root string) (Space, error) {
	if operand == root {
		return Space{}, &NotASpaceError{Operand: operand, Root: root}
	}

	label := withoutRootSuffix(operand, root)
	if strings.Contains(label, ".") {
		return Space{}, &NotASpaceError{Operand: operand, Root: root}
	}
	if !ValidLabel(label) {
		return Space{}, &InvalidLabelError{Operand: operand}
	}
	return Space{Label: label, Domain: label + "." + root}, nil
}

// ParseApp parses an abbreviated or fully qualified app-on-space operand.
func ParseApp(operand, root string) (App, error) {
	if operand == root {
		return App{}, &NotAnAppError{Operand: operand}
	}

	remainder := withoutRootSuffix(operand, root)
	pieces := strings.Split(remainder, ".")
	if len(pieces) != 2 {
		return App{}, &NotAnAppError{Operand: operand}
	}
	if !ValidLabel(pieces[0]) || !ValidLabel(pieces[1]) {
		return App{}, &InvalidLabelError{Operand: operand}
	}
	if !appref.ValidName(pieces[0]) {
		return App{}, &UnusableAppError{Name: pieces[0]}
	}

	space, err := Parse(pieces[1], root)
	if err != nil {
		return App{}, err
	}
	return App{
		Name:     pieces[0],
		Space:    space,
		Hostname: pieces[0] + "." + space.Domain,
	}, nil
}

func withoutRootSuffix(operand, root string) string {
	return strings.TrimSuffix(operand, "."+root)
}

func asciiLowerOrDigit(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9'
}
