// Command wiki is the loopback-only wiki MCP service behind nginx.
package main

import "wiki/internal/wiki"

func main() {
	wiki.Main()
}
