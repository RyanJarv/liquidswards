# liquidswards

## What does it do?

* Recursively discovers and enumerates access to sts:AssumeRole.
* Makes pretty graphs of accessible roles using GraphViz.

**IMPORTANT**: This tool is relatively new and hasn't had a ton of testing. Make sure you understand what it does
before using it.

![Screen Shot 2022-01-20 at 11 04 21 PM](https://user-images.githubusercontent.com/4079939/150481797-1172bd65-1779-497d-a77e-78a6956ce117.png)


https://user-images.githubusercontent.com/4079939/152632976-813343ff-f89d-46b3-80e9-20ee3e9bcff0.mov

## Why?

I wanted a way of mapping assume role paths without depending on IAM. Having studied the code of the several
open-source tools for doing this and written my own, I know this is not easy; additionally, in some edge cases, it's
also not possible. This tool started with a desire to have an alternative to [PMapper](https://github.com/nccgroup/PMapper)
and [AWSpx](https://github.com/FSecureLABS/awspx) that does something similar (specifically for sts:AssumeRole) in the
stupidest possible way. Altogether avoiding IAM parsing allows you to ditch a lot of complexity, have higher
confidence in the results (because of reduced complexity, *not* because other tools are undependable), and may work in
various cases that do not apply to different approaches. The original goal would probably be best thought of as a way
to validate the results of other tools, *not a replacement*. Both PMapper and AWSpx are excellent tools, and they are
likely what you want if you need to discover escalation paths in your accounts.

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
  -debug
    	Enable debug output
  -load
    	Load results from previous scans.
  -name string
    	Name of environment, used to store and retrieve graphs. (default "default")
  -no-save
    	Do not save scan results to disk.
  -no-scan
    	Do not attempt to assume file any file.
  -profiles string
    	List of AWS profiles (seperated by commas)
  -region string
    	The AWS Region to use (default "us-east-1")
  -scope string
    	List of AWS account ID's (seperated by comma's) that are in scope. 
    	Accounts associated with any profiles used are always in scope 
    	regardless of this value.
```

Discover roles across several profiles by calling iam:ListRoles. This will enumerate roles via iam:ListRoles using
the profile `ryanjarv` as well as any role it can assume from that profile either directly or indirectly.

```sh
liquidswards -name session_name -profiles aws_profile_1,aws_profile_2
```

### What's with the name?

It's named after the best solo Wu-Tang album.

<img width="400" alt="liquidswords" src="https://user-images.githubusercontent.com/4079939/150443336-621ff008-e3a4-48bd-b871-0bb6afc8716b.jpg">
