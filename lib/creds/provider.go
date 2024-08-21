// Package stscreds are credential Providers to retrieve STS AWS credentials.
//
// STS provides multiple ways to retrieve credentials which can be used when making
// future AWS service API operation calls.
//
// The SDK will ensure that per instance of credentials.Credentials all requests
// to refresh the credentials will be synchronized. But, the SDK is unable to
// ensure synchronous usage of the AssumeRoleProvider if the value is shared
// between multiple Credentials or service clients.
//
// # Assume Role
//
// To assume an IAM role using STS with the SDK you can create a new Credentials
// with the SDKs's stscreds package.
//
//	// Initial credentials loaded from SDK's default credential chain. Such as
//	// the environment, shared credentials (~/.aws/credentials), or EC2 Instance
//	// Role. These credentials will be used to to make the STS Assume Role API.
//	cfg, err := config.LoadDefaultConfig(context.TODO())
//	if err != nil {
//		panic(err)
//	}
//
//	// Create the credentials from AssumeRoleProvider to assume the role
//	// referenced by the "myRoleARN" ARN.
//	stsSvc := sts.NewFromConfig(cfg)
//	creds := stscreds.NewAssumeRoleProvider(stsSvc, "myRoleArn")
//
//	cfg.Credentials = aws.NewCredentialsCache(creds)
//
//	// Create service client value configured for credentials
//	// from assumed role.
//	svc := s3.NewFromConfig(cfg)
//
// # Assume Role with custom MFA Token provider
//
// To assume an IAM role with a MFA token you can either specify a custom MFA
// token provider or use the SDK's built in StdinTokenProvider that will prompt
// the user for a token code each time the credentials need to to be refreshed.
// Specifying a custom token provider allows you to control where the token
// code is retrieved from, and how it is refreshed.
//
// With a custom token provider, the provider is responsible for refreshing the
// token code when called.
//
//		cfg, err := config.LoadDefaultConfig(context.TODO())
//		if err != nil {
//			panic(err)
//		}
//
//	 staticTokenProvider := func() (string, error) {
//	     return someTokenCode, nil
//	 }
//
//		// Create the credentials from AssumeRoleProvider to assume the role
//		// referenced by the "myRoleARN" ARN using the MFA token code provided.
//		creds := stscreds.NewAssumeRoleProvider(sts.NewFromConfig(cfg), "myRoleArn", func(o *stscreds.AssumeRoleOptions) {
//			o.SerialNumber = aws.String("myTokenSerialNumber")
//			o.TokenProvider = staticTokenProvider
//		})
//
//		cfg.Credentials = aws.NewCredentialsCache(creds)
//
//		// Create service client value configured for credentials
//		// from assumed role.
//		svc := s3.NewFromConfig(cfg)
//
// # Assume Role with MFA Token Provider
//
// To assume an IAM role with MFA for longer running tasks where the credentials
// may need to be refreshed setting the TokenProvider field of AssumeRoleProvider
// will allow the credential provider to prompt for new MFA token code when the
// role's credentials need to be refreshed.
//
// The StdinTokenProvider function is available to prompt on stdin to retrieve
// the MFA token code from the user. You can also implement custom prompts by
// satisfying the TokenProvider function signature.
//
// Using StdinTokenProvider with multiple AssumeRoleProviders, or Credentials will
// have undesirable results as the StdinTokenProvider will not be synchronized. A
// single Credentials with an AssumeRoleProvider can be shared safely.
//
//	cfg, err := config.LoadDefaultConfig(context.TODO())
//	if err != nil {
//		panic(err)
//	}
//
//	// Create the credentials from AssumeRoleProvider to assume the role
//	// referenced by the "myRoleARN" ARN using the MFA token code provided.
//	creds := stscreds.NewAssumeRoleProvider(sts.NewFromConfig(cfg), "myRoleArn", func(o *stscreds.AssumeRoleOptions) {
//		o.SerialNumber = aws.String("myTokenSerialNumber")
//		o.TokenProvider = stscreds.StdinTokenProvider
//	})
//
//	cfg.Credentials = aws.NewCredentialsCache(creds)
//
//	// Create service client value configured for credentials
//	// from assumed role.
//	svc := s3.NewFromConfig(cfg)
package creds

import (
	"context"
	"fmt"
	"github.com/RyanJarv/liquidswards/lib/graph"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
)

type GraphProvider struct {
	utils.Context
	Arn       string
	Providers map[string]*aws.CredentialsCache
	Graph     *graph.Graph[*Config]
	Client    stscreds.AssumeRoleAPIClient
}

// NewGraphProvider constructs and returns a credentials provider that will retrieve credentials by assuming a IAM role using STS.
func NewGraphProvider(ctx utils.Context, graph *graph.Graph[*Config], arn string) *aws.CredentialsCache {
	return aws.NewCredentialsCache(&GraphProvider{Context: ctx, Graph: graph, Arn: arn}, func(opt *aws.CredentialsCacheOptions) {
		opt.ExpiryWindowJitterFrac = 0.7
	})
}

func (p *GraphProvider) Retrieve(ctx context.Context) (creds aws.Credentials, err error) {
	node, ok := p.Graph.GetNode(p.Arn)
	if !ok {
		return creds, fmt.Errorf("unable to find node for %s", p.Arn)
	}

	for _, src := range node.Inbound() {
		provider := stscreds.NewAssumeRoleProvider(src.Value().Sts, p.Arn, func(o *stscreds.AssumeRoleOptions) {
			o.RoleSessionName = "liquidswards"
		})
		if creds, err = provider.Retrieve(ctx); err != nil {
			p.Info.Printf("failed to assume role %s: %s", p.Arn, err)
			continue
		} else {
			break
		}
	}

	return creds, err
}
