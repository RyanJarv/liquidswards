package plugins

import (
	"fmt"
	"github.com/RyanJarv/liquidswards/lib/creds"
	"github.com/RyanJarv/liquidswards/lib/types"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"sync"
)

func NewList(_ utils.Context, args types.GlobalPluginArgs) types.Plugin {
	return &List{
		GlobalPluginArgs: args,
		accountMap:       &sync.Map{},
	}
}

type List struct {
	types.GlobalPluginArgs
	accountMap *sync.Map
}

func (l *List) Name() string { return "list" }
func (l *List) Enabled() (bool, string) {
	return true, "will call iam.ListRoles to discover roles"
}

func (l *List) Run(ctx utils.Context) {
	l.Access.Walk(func(cfg *creds.Config) {
		if _, ok := l.accountMap.Load(cfg.Account()); ok {
			ctx.Debug.Println("already listed roles in", cfg.Account())
			return
		}
		l.accountMap.Store(cfg.Account(), 1)

		if err := ForEachRole(ctx, cfg.Config(), func(r types.Role) {
			if l.Scope != nil && !utils.ArnInScope(l.Scope, *r.Arn) {
				ctx.Debug.Println("not in scope, skipping:", *r.Arn)
				return
			}

			l.FoundRoles.Add(r)
			ctx.Debug.Println("list roles: found:", r.Id())
		}); err != nil {
			ctx.Error.Println("error listing roles:", err)
		}
	})
}

func ForEachRole(ctx utils.Context, awsCfg aws.Config, found func(item types.Role)) error {
	svc := iam.NewFromConfig(awsCfg)
	paginator := iam.NewListRolesPaginator(svc, &iam.ListRolesInput{})

	for paginator.HasMorePages() {
		resp, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to retrieve Items: %w", err)
		}

		for _, role := range resp.Roles {
			found(types.Role{Role: role})
		}
	}

	return nil
}
