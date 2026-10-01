//go:build full

package playwright

import (
	"context"
	"fmt"

	"github.com/go-rod/rod"
)

func (c *Command) execScroll(ctx context.Context, args []string) (string, error) {
	if len(args) != 2 || (args[1] != "up" && args[1] != "down") {
		return "", fmt.Errorf("usage: playwright scroll <session> <up|down>")
	}
	sess, err := c.getSession(args[0])
	if err != nil {
		return "", err
	}
	return sess.withPage(ctx, func(page *rod.Page) (string, error) {
		direction := 1
		if args[1] == "up" {
			direction = -1
		}
		_, err := page.Eval(`direction=>window.scrollBy(0,direction*innerHeight*0.75)`, direction)
		return "Scrolled " + args[1], err
	})
}
