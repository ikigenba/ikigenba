package checkout

import "path/filepath"

// Path returns a cleaned path beneath the checkout root.
func (checkout *Checkout) Path(elem ...string) string {
	if len(elem) == 0 {
		return checkout.Root
	}
	return filepath.Join(append([]string{checkout.Root}, elem...)...)
}
