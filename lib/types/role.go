package types

import (
	"encoding/json"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"log"
	"net/url"
	"strings"
)

func NewRole(arn string) Role {
	return Role{
		Role: types.Role{
			Arn: aws.String(arn),
		},
	}
}

type Role struct {
	types.Role
	externalID *string
}

func (r Role) Id() string {
	return *r.Arn
}

// AssumeRolePolicyDocument may look like this:
//
// TODO: Check for other variations of this.
//
//	{
//	    "Version": "2012-10-17",
//	    "Statement": [
//	        {
//	            "Effect": "Allow",
//	            "Principal": {
//	                "AWS": "arn:aws:iam::336983520827:root"
//	            },
//	            "Action": "sts:AssumeRole",
//	            "Condition": {}
//	        }
//	    ]
//	}
type AssumeRolePolicyDocument struct {
	Statement []struct {
		Principal struct {
			// AWS can be a string or a list, RawMessage will leave it as []byte.
			AWS json.RawMessage
		}
		Condition *struct {
			StringEqual *struct {
				ExternalId *string `json:"sts:ExternalId"`
			}
		}
	}
}

func (r *Role) ExternalID() *string {
	// Return the manually set ExternalID if it exists.
	if r.externalID != nil {
		return r.externalID
	}

	return findExternalId(*r.AssumeRolePolicyDocument, *r.Arn)
}

// TODO: Add caching
func findExternalId(doc string, arn string) *string {
	decoded, err := url.QueryUnescape(doc)
	if err != nil {
		log.Printf("error url decoding role trust policy: %s\n", err)
		return nil
	}

	var trust AssumeRolePolicyDocument
	err = json.Unmarshal([]byte(decoded), &trust)
	if err != nil {
		log.Printf("error parsing role trust policy: %s\n", err)
		// TODO: This is probably error prone, need testing
		return nil
	}

	// Multiple statements can exist, not all of them are relevant.
	for _, stmt := range trust.Statement {

		// Does this statement apply to us?
		if strings.Contains(string(stmt.Principal.AWS), arn) {

			// TODO: This may not catch all cases.
			// Check if Condition.StringEquals.ExternalId is missing.
			if stmt.Condition == nil || stmt.Condition.StringEqual == nil || stmt.Condition.StringEqual.ExternalId == nil {
				// No external ID is needed
				return nil
			}

			// Return the externalId found in the trust policy.
			return stmt.Condition.StringEqual.ExternalId
		}
	}

	// Assume no externalId
	return aws.String("")
}
