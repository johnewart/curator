# curator

A supervisord-like process manager written in Go for portability and to
remove the need for an interpreter. 

## Features
* HTTP RPC
* Reload config on demand
* Start / stop processes via HTTP
* Load YAML files like the ones provided
* Template variables in command strings, process names and log file paths

## Roadmap
* HTML dashboard via HTTP
* Force reload processes on reload of configuration (if requested)
* Metrics emitting

## Authors

* John Ewart <john@johnewart.net>


## License

Copyright 2014-2015 John Ewart <john@johnewart.net>. Released under the Apache 2.0 license. See the file LICENSE for further details.
