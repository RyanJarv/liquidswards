package plugins

import (
	"github.com/RyanJarv/liquidswards/lib/types"
	"github.com/RyanJarv/liquidswards/lib/utils"
)

import (
	"fmt"
	"github.com/RyanJarv/liquidswards/lib/creds"
	"strings"
)

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
	return true, "will assume roles discovered by the scanner"
}

func (a *Assume) Run(ctx utils.Context) {
	a.Access.Walk(func(cfg *creds.Config) {
		ctx.Debug.Printf("running scan on %s", strings.Join(cfg.IdentityPath(), " -> "))

		verifyScope(a.Scope, cfg.Arn())

		a.FoundRoles.Walk(func(role types.Role) {
			if ctx.IsDone("Finished assuming Items, exiting...") {
				return
			}

			verifyScope(a.Scope, *role.Arn)

			newCfg, err := cfg.Assume(ctx, *role.Arn)
			if err != nil {
				ctx.Debug.Println(err)
			} else {
				a.Access.Add(newCfg)
				a.Graph.AddEdge(cfg, newCfg)

				ctx.Info.Printf("%s"+utils.Arrow+"%s", strings.Join(cfg.IdentityPath(), utils.Arrow), *role.Arn)
			}
		})
	})
}

func verifyScope(scope []string, arn string) {
	if scope != nil && !utils.ArnInScope(scope, arn) {
		// Senders should check if the ARN is in scope, exit to avoid traversing into out of scope accounts.
		panic(fmt.Errorf("assume roles: exiting due out of scope arn (this is a bug): %s\n", arn))
	}
}
