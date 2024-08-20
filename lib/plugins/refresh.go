package plugins

import (
	"fmt"
	"github.com/RyanJarv/liquidswards/lib/creds"
	"github.com/RyanJarv/liquidswards/lib/types"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"time"
)

var refresh = utils.PluginArgs.Int("refresh", 0, `
The refresh rate used for the access plugin in seconds. This defaults to once an hour, but if you want to bypass role 
revocation without using cloudtrail events (-sqs-queue option, see the README for more info) you can set this to 
approximately three seconds.
`)

type NewAccessInput struct {
	types.GlobalPluginArgs
	Context utils.Context

	// By the time session revocation through the UI is processed it appears to limit sessions for about 6 seconds
	// in the past. This gives us a 6-second window where new sessions are not affected in any way.
	AccessRefresh int
}

func NewRefresh(_ utils.Context, args types.GlobalPluginArgs) types.Plugin {
	return &Refresh{
		GlobalPluginArgs: args,
		RefreshSeconds:   *refresh,
	}
}

type Refresh struct {
	types.GlobalPluginArgs
	RefreshSeconds int
}

func (a *Refresh) Name() string {
	return "refresh"
}

func (a *Refresh) Enabled() (bool, string) {
	if a.RefreshSeconds > 0 {
		return true, fmt.Sprintf("will refresh credentials every %d seconds", a.RefreshSeconds)
	} else {
		return false, "no -refresh arg provided"
	}
}

func (a *Refresh) Run(ctx utils.Context) {
	a.Access.Walk(func(cfg *creds.Config) {
		run(ctx, cfg, a.RefreshSeconds)
	})
}

// run refreshes credentials periodically and when triggered by the returned channel.
//
// This allows us to catch and refresh revoked sessions, the time between refreshes is configured with -access-refresh.
func run(ctx utils.Context, cfg *creds.Config, seconds int) {
	utils.SetDebugLabels("plugins", "access", "identity", cfg.Arn())

	go func() {
		defer func() {
			if r := recover(); r != nil {
				ctx.Error.Println("access:", r)
			}
		}()
		for {
			select {
			case <-time.After(time.Second * time.Duration(seconds)):
			}
			ctx.Info.Println("refreshing:", cfg.Arn())

			_, err := cfg.Refresh(ctx)
			if err != nil {
				ctx.Error.Printf("periodic refresh failed: %s\n", err)
				continue
			}
			ctx.Info.Println("refreshed:", cfg.Arn())
		}
	}()
}
