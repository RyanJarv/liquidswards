# liquidswards

## What does it do?

* Recursively discovers and enumerates access to sts:AssumeRole.
  * Can find difficult to access and unknown roles across accounts.
* Discovers new roles to enumerate access via iam:ListRoles, CloudTrail (previous sts:AssumeRole calls), or a provided file.
  * Read access to IAM is not necessarily required.
* Maintains access to discovered roles via role juggling.
  * Can bypass role session revocation and credential expiration via role juggling.
  * Uses the internally built roles graph to find the shortest possible paths for credential juggling.
* Makes pretty graphs of accessible roles using GraphViz.

### v0.3.0

This version adds the ability to listen to session revocation events from and SNS queue. Which allows bypassing session
revocation without needing to call AssumeRole nearly as often.

Previously the tool simply cycled through the possible paths independent of other active credentials. Now this uses the nearest
active neighbor to refresh the session. This also happens in the credential provider of aws.Config, in theory this could
be used in other go code and the credentials would transparently be refreshed from the graph as needed.

TODO: Include info on setting events up.


**IMPORTANT**: This tool is relatively new and hasn't had a ton of testing. Make sure you
understand what it does before using it.

![Screen Shot 2022-01-20 at 11 04 21 PM](https://user-images.githubusercontent.com/4079939/150481797-1172bd65-1779-497d-a77e-78a6956ce117.png)


https://user-images.githubusercontent.com/4079939/152632976-813343ff-f89d-46b3-80e9-20ee3e9bcff0.mov



## Why?

Initially, I wanted a way of mapping assume role paths without depending on IAM. Having studied the code of the several
open-source tools for doing this and written my own, I know this is not easy; additionally, in some edge cases, it's 
also not possible. This tool started with a desire to have an alternative to [PMapper](https://github.com/nccgroup/PMapper)
and [AWSpx](https://github.com/FSecureLABS/awspx) that does something similar (specifically for sts:AssumeRole) in the 
stupidest possible way. Altogether avoiding IAM parsing allows you to ditch a lot of complexity, have higher
confidence in the results (because of reduced complexity, *not* because other tools are undependable), and may work in 
various cases that do not apply to different approaches. The original goal would probably be best thought of as a way 
to validate the results of other tools, *not a replacement*. Both PMapper and AWSpx are excellent tools, and they are 
likely what you want if you need to discover escalation paths in your accounts.

Anyway, that's just how this started; since then, liquidswards has developed into more of a pentesting tool. You can 
still use the tool in the way described above; in many ways, it's the same goal, but since the original version, it 
also supports:

* Additional methods of discovering roles:
  * Searching CloudTrail sts:AssumeRole logs
    * Turn this on with `-cloudtrail`
  * User-provided text file with one ARN per line
    * Specified with `-file <role file>`
* Maintaining access via role juggling
  * Can bypass role session revocation in the console and credential expirations
  * Turn this on with `-access`
* Retrieve credentials for any IAM role in the graph.
  * `export $(liquidswards <role arn>)`

## Role Juggling

Role juggling can be considered a form of [role chaining](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles_terms-and-concepts.html#iam-term-role-chaining)
that occurs when the chained roles form a cycle.

To perform credential juggling liquidswards uses the internal graph to discover cycles in assume role paths. When the
`-access` flag is passed, dynamic role juggling will be run for any affected roles.

### Bypassing Role Revocation

When active roles can assume each other it is possible to bypass role revocation when used manually in the web console.

This is because there will be a delay between the manual revocations, allowing either one to refresh the lost access.

### Bypassing Session Expiration

When role juggling is used, access is maintained by assuming IAM roles in a loop continuously. Each time a new role
is assumed the expiration time is refreshed.

## Other Somewhat Interesting Things

* The sts:AssumeRole action is considered a read only action, and will not show up if you are only looking at write
  actions in CloudTrail (the default).
* The Quarantine policy that is applied to credentials found on GitHub does not block sts:AssumeRole.

## Install

Currently, there are release binaries for:

* Darwin -- x86_64 and amd64
* Linux -- x86_64
* Windows -- x86_64

The following will install the binaries on Darwin and Linux:

```
tag=$(curl -s https://api.github.com/repos/RyanJarv/liquidswards/releases/latest|jq -r '.tag_name'|tr -d 'v')
curl -L "https://github.com/RyanJarv/liquidswards/releases/download/v${tag}/liquidswards_${tag}_$(uname -s)_$(uname -m).tar.gz" \
    | tar -xvf - -C /usr/local/bin/ liquidswards
```

## Build

This project requires go 1.18 or later.

```
% go build -o liquidswards main.go
% ./liquidswards -h
```

## Help

```
% liquidswards -h
Usage of liquidswards:
  -access
    	Enable the maintain access plugin. This will attempt to maintain access to the discovered file through
        Role juggling.
  -access-refresh int
    	The refresh rate used for the accessplugin in seconds. This defaults to once an hour, but if you want
     to bypass role revocation without usingcloudtrail events (-sqs-queue option, see the README for more
     info) you can set this to approximately three seconds. (default 3600)
  -cloudtrail
    	Enable the CloudTrail plugin. This will attempt to discover new IAM Roles by searching for previous
        sts:AssumeRole API calls in CloudTrail.
  -debug
    	Enable debug output
  -file string
    	A file containing a list of additional file to enumerate.
  -load
    	Load results from previous scans.
  -max-per-second int
    	Maximum requests to send per second.
  -name string
    	Name of environment, used to store and retrieve graphs. (default "default")
  -no-save
    	Do not save scan results to disk.
  -no-scan
    	Do not attempt to assume file any file.
  -no-scope
    	Disable scope, all discovered role ARN's belonging to ANY account 
    	will be enumerated for access and additional file recursively.
    	
    	IMPORTANT: Use caution, this can lead to a *LOT* of unintentional 
    	access if you are (un)lucky.
    	
    	TODO: Add a mode that tests for discovered roles in other accounts 
    	but does not recursively search them.
  -profiles string
    	List of AWS profiles (seperated by commas)
  -region string
    	The AWS Region to use (default "us-east-1")
  -scope string
    	List of AWS account ID's (seperated by comma's) that are in scope. 
    	Accounts associated with any profiles used are always in scope 
    	regardless of this value.
```

Discover roles across several profiles by calling iam:ListRoles. This will enumerate roles via
iam:ListRoles using the profile `ryanjarv` as well as any role it can assume from that profile
either directly or indirectly.

```sh
liquidswards -name ryanjarv -profiles ryanjarv
```

Use credentials of a discovered role. This will traverse the previously generated graph to access roles that
may not be directly accessible by your current user. Any enumerations and scans mentioned above are skipped.

```sh
export $(liquidswards -name ryanjarv -profiles ryanjarv arn:aws:iam::457260041085:role/readonly)
aws sts get-caller-identity
```

#### YOLO

liquidswards -name ryanjarv -profiles ryanjarv -access -cloudtrail -no-scope

### What's with the name?

It's named after the best solo Wu-Tang album.

<img width="400" alt="liquidswords" src="https://user-images.githubusercontent.com/4079939/150443336-621ff008-e3a4-48bd-b871-0bb6afc8716b.jpg">
