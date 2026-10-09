package gateway

import "github.com/ikigenba/ikigenba/appkit/page"

// Description is the gateway's description in its manifest and about screen.
const Description string = "Connect AI assistants to your services"

// AboutData supplies the about screen's banner and description.
type AboutData struct {
	Banner      page.Banner
	Description string
}
