package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/RyanJarv/liquidswards/lib/graph"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"io"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"strings"
)

var (
	region       string
	maxPerSecond int
	profilesStr  string
	save         bool
	load         bool
	path         string
	force        bool
	debug        bool
	l            = utils.L{
		Info:  log.New(os.Stdout, utils.Green.Color("[INFO] "), 0),
		Debug: log.New(os.Stderr, utils.Gray.Color("[DEBUG] "), 0),
		Error: log.New(os.Stderr, utils.Red.Color("[ERROR] "), 0),
	}
)

func main() {
	flag.StringVar(&region, "us-east-1", "", "The AWS Region to use")
	flag.IntVar(&maxPerSecond, "max-per-second", 0, "Maximum requests to send per second.")
	flag.StringVar(&profilesStr, "profiles", "", "List of AWS profiles (seperated by commas) to use for discovering roles.")
	flag.BoolVar(&save, "save", false, "Save list of roles to file specified by path, do not attempt to assume them.")
	flag.BoolVar(&load, "load", false, "Load list of roles to file specified by path then attempt to assume them.")
	flag.BoolVar(&force, "force", false, "Overwrite file specified by -path if it exists.")
	flag.StringVar(&path, "path", "", "Path to use for storing the role list.")
	flag.BoolVar(&debug, "debug", false, "Enable debug output")
	flag.Parse()

	l.Debug.Printf("using region %s\n", region)

	if !debug {
		null, err := os.Open(os.DevNull)
		if err != nil {
			l.Error.Fatalln(err)
		}
		l.Debug.SetOutput(null)
	}

	if len(flag.Args()) > 0 {
		l.Error.Fatalln("extra arguments detected, did you mean to pass a comma seperated list to -profiles instead?")
	}

	if (!save && !load) || (save && load) {
		l.Error.Fatalln("must specify either (but not both) -load or -save.")
	}

	if path == "" {
		l.Error.Fatalln("must specify a location for role arns with -path.")
	}

	ctx := context.Background()
	cfgs, err := parseProfiles(ctx, profilesStr, maxPerSecond)
	if err != nil {
		log.Fatalln(err)
	}

	if save {
		if !force {
			if _, err := os.Stat(path); os.IsExist(err) {
				l.Error.Fatalf("the file %s already exists, use -force to overwrite it.\n", path)
			}
		}
		err := Save(ctx, cfgs, path)
		if err != nil {
			l.Error.Fatalln(err)
		}
	} else if load {
		roles, err := Load(path)
		if err != nil {
			l.Error.Fatalln(err)
		}

		assume := AssumeAllRoles(ctx, roles)
		for _, cfg := range cfgs {
			err := assume.Run(cfg)
			if err != nil {
				l.Error.Fatalln(err)
			}
		}

		assume.PrintGraph()

	} else {
		l.Error.Fatalln("something went wrong")
	}

}

func parseProfiles(ctx context.Context, profiles string, second int) ([]*aws.Config, error) {
	limiter := utils.NewSessionLimiter(second)

	var ret []*aws.Config
	for _, p := range strings.Split(profiles, ",") {
		p = strings.Trim(p, " \t\n")
		cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(p))
		if err != nil {
			return nil, fmt.Errorf("failed loading profile %s using regin %s: %w", p, region, err)
		}

		limiter.Instrument(&cfg)

		ret = append(ret, &cfg)
	}
	return ret, nil
}

func AssumeAllRoles(ctx context.Context, roles []string) *AssumeRoles {
	return &AssumeRoles{
		graph: graph.NewDirectedGraph[string](),
		ctx:   ctx,
		roles: roles,
	}
}

type AssumeRoles struct {
	graph *graph.Graph[string]
	ctx   context.Context
	roles []string
}

func (a *AssumeRoles) Run(cfg *aws.Config) error {
	resp, err := sts.NewFromConfig(*cfg).GetCallerIdentity(a.ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return fmt.Errorf("calling sts get-caller-identity failed: %w", err)
	}
	a.assumeRoles(cfg, []string{utils.CleanArn(*resp.Arn)})
	return nil
}

func (a *AssumeRoles) assumeRoles(cfg *aws.Config, identity []string) {
	l.Debug.Printf("running assumeRoles on %s", strings.Join(identity, " -> "))
	currArn := identity[len(identity)-1]
	if len(identity) == 60 {
		l.Info.Printf("max depth of 50 reached when enumerating %s\n", currArn)
		return
	}

	svc := sts.NewFromConfig(*cfg)
	for _, role := range a.roles {
		if utils.VisitedRole(identity, role) {
			continue
		}
		_, err := svc.AssumeRole(a.ctx, &sts.AssumeRoleInput{
			RoleArn:         aws.String(role),
			RoleSessionName: aws.String("rhino-assumerole-mapping"),
			DurationSeconds: aws.Int32(900),
			ExternalId:      nil, // TODO: pass external ID along with role ARN
		})

		if err != nil {
			l.Debug.Println(err.Error())
			continue
		}
		a.graph.AddNode(role)
		a.graph.AddEdge(currArn, role, aws.Bool(true), nil)
		l.Info.Printf("%s "+utils.Cyan.Color("--assumed role--> ")+" %s", identity, role)

		newCfg := *cfg
		newCfg.Credentials = stscreds.NewAssumeRoleProvider(sts.NewFromConfig(*cfg), role)

		go a.assumeRoles(&newCfg, append(identity, role))
	}
}

func (a *AssumeRoles) PrintGraph() {
	fmt.Println(utils.Green.Color("\nAccessed:"))
	for _, start := range a.graph.Nodes {
		if len(start.Edges) == 0 {
			continue
		}

		graph.DFS[string](a.graph, start, func(e *graph.Edge[string]) bool {
			return e.Accessed != nil && *e.Accessed
		}, nil, []string{}, func(node string, path []string) {
			fmt.Printf("\n")
			for i := 0; i < len(path); i++ {
				fmt.Printf("\t")
			}
			if len(path) == 0 {
				fmt.Printf(" "+utils.Cyan.Color("*")+" %s", node)
			} else {
				fmt.Printf(utils.Cyan.Color("->")+" %s", node)
			}
		})
	}
	fmt.Printf("\n")
}

func Load(path string) ([]string, error) {
	text, err := ioutil.ReadFile(path)
	if err != nil {
		return []string{}, fmt.Errorf("failed to open file %s: %w", path, err)
	}
	text = bytes.Trim(text, "\n")

	return strings.Split(string(text), "\n"), nil
}

// AssumeRolePolicyDocument
//
// 	Example Json:
//			  {
//                "Version": "2012-10-17",
//                "Statement": [
//                    {
//                        "Effect": "Allow",
//                        "Principal": {
//                            "Service": "ssm.amazonaws.com"
//                        },
//                        "Action": "sts:AssumeRole"
//                    }
//                ]
//            },
//
type AssumeRolePolicyDocument struct {
	Version   string `json:"Version"`
	Statement []struct {
		Effect    string `json:"Effect"`
		Principal struct {
			Service   *string `json:"Service"`
			Aws       *string `json:"Aws"`
			Federated *string `json:"Federated"`
		} `json:"Principal"`
	} `json:"Statement"`
}

func filter(role types.Role) bool {
	policyDoc := AssumeRolePolicyDocument{}
	policyStr, err := url.QueryUnescape(*role.AssumeRolePolicyDocument)
	if err != nil {
		l.Error.Printf("could not unescape trust policy: %s\n", err)
		return true
	}
	err = json.Unmarshal([]byte(policyStr), &policyDoc)
	if err != nil {
		l.Info.Printf("failed to unmarshal role %s\n", *role.Arn)
		l.Debug.Printf("role trust policy: %s\n", policyStr)
		return true
	}

	for _, s := range policyDoc.Statement {
		if s.Principal.Aws != nil || s.Principal.Federated != nil {
			return true
		}
	}
	l.Info.Printf("skipping role %s because it does not match the filter function\n", *role.Arn)
	l.Info.Printf("trust policy is %s\n", policyStr)
	return false
}

func Save(ctx context.Context, cfgs []*aws.Config, path string) error {
	file, err := os.Create(path)
	defer file.Close()
	if err != nil {
		return fmt.Errorf("failed to create file at %s", path)
	}
	for _, cfg := range cfgs {
		err := WriteRolesToFile(ctx, cfg, file, filter)
		if err != nil {
			return fmt.Errorf("failed writing to %s: %w", path, err)
		}
	}
	return nil
}

func WriteRolesToFile(ctx context.Context, cfg *aws.Config, out io.Writer, filter func(types.Role) bool) error {
	svc := iam.NewFromConfig(*cfg)

	resp := &iam.ListRolesOutput{IsTruncated: true}

	var err error
	for err == nil && resp.IsTruncated {
		resp, err = svc.ListRoles(ctx, &iam.ListRolesInput{Marker: resp.Marker})
		if err != nil {
			return fmt.Errorf("failed listing roles: %w", err)
		}
		for _, role := range resp.Roles {
			if !filter(role) {
				continue
			}
			_, err := io.WriteString(out, fmt.Sprintln(*role.Arn))
			if err != nil {
				return fmt.Errorf("failed writing role arn to output: %w", err)
			}
		}
	}
	return nil
}
