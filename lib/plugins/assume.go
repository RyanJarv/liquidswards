package plugins

import (
	"flag"
	"github.com/RyanJarv/liquidswards/lib/types"
	"github.com/RyanJarv/liquidswards/lib/utils"
)

import (
	"fmt"
	"github.com/RyanJarv/liquidswards/lib/creds"
	"strings"
)

var noAssume = flag.Bool("no-assume", false, "do not attempt to assume discovered roles")

func NewAssume(ctx utils.Context, args types.GlobalPluginArgs) types.Plugin {
	return &Assume{
		GlobalPluginArgs: args,
	}
}

type Assume struct {
	types.GlobalPluginArgs

	// For mocking assumeRole which gets set in Register
	AssumeRole func(ctx utils.Context, cfg *creds.Config, role types.Role)
}

func (a *Assume) Name() string { return "Assume" }
func (a *Assume) Enabled() (bool, string) {
	if *noAssume {
		return false, "assuming roles is disabled because -no-assume was used"
	} else {
		return true, "assuming roles discovered by the scanner"
	}
}

func (a *Assume) Run(ctx utils.Context) {
	a.Access.Walk(func(cfg *creds.Config) {
		ctx.Debug.Println("assume:", strings.Join(cfg.IdentityPath(), " -> "))

		verifyScope(a.Scope, cfg.Arn())

		if cfg.IsRecursive() {
			ctx.Debug.Println("assume: skipping recursive chain:", cfg.Arn())
			return
		}

		a.FoundRoles.Walk(func(role types.Role) {
			ctx.Debug.Printf("assume: testing: %s -> %s", cfg.Id(), role.Id())
			if ctx.IsDone("Finished assuming Items, exiting...") {
				return
			}

			verifyScope(a.Scope, *role.Arn)

			newCfg, err := cfg.Assume(ctx, *role.Arn)
			if err != nil {
				ctx.Debug.Println(err)
				return
			}

			a.Access.Add(newCfg)
			ctx.Info.Println(strings.Join(newCfg.IdentityPath(), utils.Arrow))
		})
	})
}

func verifyScope(scope []string, arn string) {
	if scope != nil && !utils.ArnInScope(scope, arn) {
		// Senders should check if the ARN is in scope, exit to avoid traversing into out of scope accounts.
		panic(fmt.Errorf("assume roles: exiting due out of scope arn (this is a bug): %s\n", arn))
	}
}
