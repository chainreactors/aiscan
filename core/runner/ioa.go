package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/chainreactors/aiscan/cmd/ioaserve"
	cfg "github.com/chainreactors/aiscan/core/config"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/pkg/tui"
	ioaclient "github.com/chainreactors/ioa/client"
)

func RunIOAServe(ctx context.Context, option *cfg.Option, logger telemetry.Logger) error {
	return ioaserve.RunServe(ctx, ioaserve.Config{
		URL: option.IOAURL,
		DB:  "",
	}, logger)
}

func ResolveIOANodeName(option *cfg.Option) string {
	if option.IOANodeName != "" {
		return option.IOANodeName
	}
	var b [4]byte
	if _, err := rand.Read(b[:]); err == nil {
		return "aiscan-" + hex.EncodeToString(b[:])
	}
	return "aiscan-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func RunIOAClientCommand(ctx context.Context, mode cfg.RunMode, option *cfg.Option, args cfg.IOAClientArgs, logger telemetry.Logger) error {
	ioaURL := option.IOAURL
	if ioaURL == "" {
		ioaURL = "http://127.0.0.1:8765"
	}
	client, err := ioaclient.NewClient(ioaURL, "")
	if err != nil {
		return fmt.Errorf("connect to IOA server: %w", err)
	}

	switch mode {
	case cfg.RunModeIOASpaces:
		return tui.RunIOASpaces(ctx, client, option)
	case cfg.RunModeIOAMessages:
		return tui.RunIOAMessages(ctx, client, option, args)
	case cfg.RunModeIOAContext:
		return tui.RunIOAContext(ctx, client, option, args)
	case cfg.RunModeIOANodes:
		return tui.RunIOANodes(ctx, client, option, args)
	default:
		return fmt.Errorf("unknown ioa mode: %s", mode)
	}
}
