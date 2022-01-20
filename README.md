# liquidswards

<img width="914" alt="example" src="https://user-images.githubusercontent.com/4079939/150443363-1ffcd0cd-94dd-4f5a-b5ae-7810803bb7ca.png">


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

Create a graph from graphviz output.

```
circo -Goverlap=false -Tpng scan.dot -o scan.png
```

### What's with the name?

It's named after the best solo Wu-Tang album.

<img width="400" alt="liquidswords" src="https://user-images.githubusercontent.com/4079939/150443336-621ff008-e3a4-48bd-b871-0bb6afc8716b.jpg">

### But you spelled it wrong.

I disagree.
