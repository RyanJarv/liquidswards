package creds

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/RyanJarv/liquidswards/lib/graph"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/aws-sdk-go-v2/service/sts/types"
	"slices"
	"strings"
	"time"
)

type State int

const (
	ActiveState State = iota
	RefreshingState
	FailedState
)

type IConfig interface {
	MarshalJSON() ([]byte, error)
	UnmarshalJSON([]byte) error
	Id() string
	Arn() string
	Name() string
	Account() string
	Config() aws.Config
	Assume(ctx utils.Context, arn string) (*Config, error)
	Refresh(utils.Context) (*Config, error)
	SetGraph(graph interface{})
}

type SourceType int

const (
	SourceProfile SourceType = iota
	SourceAssumeRole
)

type Identity struct {
	Type   SourceType
	Name   string
	Arn    string
	Source *Identity
}

func (i Identity) Account() string {
	p := strings.Split(i.Arn, ":")

	if len(p) < 5 {
		panic(fmt.Sprintf("invalid Arn: %s", i.Arn))
	} else if len(p[4]) != 12 {
		panic(fmt.Sprintf("invalid account id: %s", p[4]))
	}

	return p[4]
}

func (i Identity) ResourceType() string {
	return strings.Split(strings.Split(i.Arn, ":")[5], "/")[0]
}

// Id returns underlying role or user ARN for this principal.
func (i Identity) Id() string {
	result := strings.Replace(i.Arn, ":sts:", ":iam:", 1)

	switch i.ResourceType() {
	case "assumed-role":
		result = strings.Replace(result, ":assumed-role/", ":role/", 1)
		result = strings.Join(strings.Split(result, "/")[:2], "/")
	default:
		result = i.Arn
	}

	return result
}

func (i *Identity) IdentityPath() (path []string) {
	identity := i
	for {
		if identity == nil {
			break
		}

		path = append(path, identity.Id())
		identity = identity.Source
	}
	slices.Reverse(path)
	return path
}

func (i Identity) IsRecursive() bool {
	return utils.SliceRepeats(i.IdentityPath())
}

func NewConfig(ctx utils.Context, region string, src Identity) (*Config, error) {
	awsCfg := aws.Config{Region: region}

	return &Config{
		Identity: src,
		Config:   awsCfg,
		ctx:      ctx,
		Sts:      sts.NewFromConfig(awsCfg),
	}, nil
}

// SetGraph needs to be called with the graph and initial creds before Config is used.
// We can't do this in NewConfig because that is used to serialize/deserialize JSON (and therefor doesn't have access
// to the graph object).
func (c *Config) SetGraph(g interface{}) {
	c.graph = g.(*graph.Graph[*Config])
	//c.cfg.Credentials = c.CredProvider(c.InitialCreds)
}

func (c *Config) SetProvider(p *aws.CredentialsCache) {
	c.Credentials = p
}

type Config struct {
	Identity
	aws.Config
	ctx   utils.Context
	Sts   stscreds.AssumeRoleAPIClient
	graph *graph.Graph[*Config]
}

func (c *Config) Assume(ctx utils.Context, arn string) (*Config, error) {
	_, err := c.Sts.AssumeRole(ctx.Context, &sts.AssumeRoleInput{
		RoleArn:         aws.String(arn),
		RoleSessionName: aws.String("liquidswards"),
	})
	if err != nil {
		return nil, fmt.Errorf("Assume(): %w", err)
	}

	newCfg, err := NewConfig(ctx, c.Region, Identity{
		Type:   SourceAssumeRole,
		Name:   arn,
		Arn:    arn,
		Source: &c.Identity,
	})
	if err != nil {
		return nil, fmt.Errorf("Assume(): %w", err)
	}

	c.graph.AddEdge(c, newCfg)
	newCfg.SetProvider(NewGraphProvider(ctx, c.graph, arn))
	newCfg.SetGraph(c.graph)

	return newCfg, err
}

// Name returns the role/user name without the path.
func (c *Config) Name() string {
	p := strings.Split(c.Arn(), "/")
	return p[len(p)-1]
}

func (c *Config) Arn() string {
	return c.Identity.Arn
}

func (c *Config) Refresh(ctx utils.Context) (aws.Credentials, error) {
	p := c.Credentials

	switch p.(type) {
	case *aws.CredentialsCache:
		p.(*aws.CredentialsCache).Invalidate()
	default:
		ctx.Info.Printf("got provider type: %T", p)
	}

	return p.Retrieve(ctx.Context)
}

type JsonConfig struct {
	Arn         string
	State       State
	Region      string
	Credentials aws.Credentials
	Identity    Identity
}

func (c *Config) MarshalJSON() ([]byte, error) {
	var creds aws.Credentials
	var err error

	if c.Credentials != nil {
		creds, err = c.Credentials.Retrieve(context.Background())
		if err != nil {
			return nil, fmt.Errorf("Save(): %w", err)
		}
	}

	obj := JsonConfig{
		Arn:         c.Arn(),
		Region:      c.Region,
		Credentials: creds,
		Identity:    c.Identity,
	}
	r, err := json.Marshal(obj)
	return r, err
}

func (c *Config) UnmarshalJSON(bytes []byte) error {
	// TODO: Implement limiter
	var obj JsonConfig
	err := json.Unmarshal(bytes, &obj)
	if err != nil {
		return fmt.Errorf("unmarshalling: %w", err)
	}

	cfg, err := NewConfig(c.ctx, obj.Region, obj.Identity)
	if err != nil {
		return fmt.Errorf("UnmarshalJSON(): %w", err)
	}

	cfg.Config.Credentials = aws.NewCredentialsCache(credentials.StaticCredentialsProvider{Value: obj.Credentials})

	*c = *cfg
	return nil
}

func ParseProfiles(ctx utils.Context, profiles string, region string, g *graph.Graph[*Config]) (configs []*Config, err error) {
	for _, p := range utils.SplitCommas(profiles) {
		awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(p))
		if err != nil {
			return nil, fmt.Errorf("loading profile %s using region %s: %w", p, region, err)
		}

		arn, err := utils.GetCallerArn(ctx, awsCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to call sts:GetCallerArn using the %s profile: %w", p, err)
		}

		cfg, err := NewConfig(ctx, region, Identity{Type: SourceProfile, Name: p, Arn: arn})
		if err != nil {
			return nil, fmt.Errorf("ParseProfiles(): %w", err)
		}

		cfg.SetProvider(awsCfg.Credentials.(*aws.CredentialsCache))

		g.AddNode(cfg)
		cfg.SetGraph(g)

		configs = append(configs, cfg)
	}
	return configs, nil
}

// ParseScope returns the accounts included in the comma delimited scopeStr as well as any accounts passed in cfgs.
func ParseScope(scopeStr string, cfgs []*Config) []string {
	scope := utils.SplitCommas(scopeStr)
	for _, cfg := range cfgs {
		scope = append(scope, cfg.Account())
	}
	return utils.FilterDuplicates(utils.RemoveDefaults(scope))
}

type MockSts struct {
	Calls []sts.AssumeRoleInput
}

func (s *MockSts) AssumeRole(ctx context.Context, in *sts.AssumeRoleInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	s.Calls = append(s.Calls, *in)

	return &sts.AssumeRoleOutput{
		AssumedRoleUser: &types.AssumedRoleUser{
			Arn:           aws.String(fmt.Sprintf("%s-Arn", *in.RoleArn)),
			AssumedRoleId: aws.String(fmt.Sprintf("%s-id", *in.RoleArn)),
		},
		Credentials: &types.Credentials{
			AccessKeyId:     aws.String("test"),
			SecretAccessKey: aws.String("test"),
			SessionToken:    aws.String("test"),
			Expiration:      aws.Time(time.Time{}),
		},
	}, nil
}

// NewTestAssumesAllConfig is kept here for now to allow using it from other modules.
func NewTestAssumesAllConfig(srcType SourceType, name string, g *graph.Graph[*Config]) (*Config, *MockSts, error) {
	src := Identity{
		Type: srcType,
		Name: name,
		Arn:  fmt.Sprintf("arn:aws:iam::123456789012:%s", name),
	}

	ctx := utils.NewContext(context.Background())
	cfg, err := NewConfig(ctx, "us-east-1", src)
	if err != nil {
		return nil, nil, fmt.Errorf("NewTestAssumesAllConfig(): %w", err)
	}

	if srcType == SourceProfile {
		creds := aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test", SessionToken: "test", Source: src.Arn}
		cfg.SetProvider(aws.NewCredentialsCache(credentials.StaticCredentialsProvider{Value: creds}))
	} else {
		cfg.SetProvider(NewGraphProvider(ctx, g, src.Arn))
	}

	cfg.SetGraph(g)

	client := &MockSts{Calls: []sts.AssumeRoleInput{}}
	cfg.Sts = client
	return cfg, client, nil
}
