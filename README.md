# liquidswards

![Example](/docs/example.png)

## Build

This project requires go 1.18 or later.

```
% go build -o liquidswards main.go
% ./liquidswards -h
```

## Help

```
% ./liquidswards -h
Usage of ./liquidswards:
  -debug
    	Enable debug output
  -force
    	Overwrite file specified by -path if it exists.
  -load
    	Load list of roles to file specified by path then attempt to assume them.
  -max-per-second int
    	Maximum requests to send per second.
  -path string
    	Path to use for storing the role list.
  -profiles string
    	List of AWS profiles (seperated by commas) to use for discovering roles.
  -save
    	Save list of roles to file specified by path, do not attempt to assume them.
  -us-east-1 string
    	The AWS Region to use
```
