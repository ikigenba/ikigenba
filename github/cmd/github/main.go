// Command github is the stateless GitHub connector behind nginx.
package main

import (
	"appkit"

	"github/internal/githubapp"
)

func main() {
	appkit.Main(githubapp.Spec())
}
