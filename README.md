# curator

A supervisord-like process manager written in Go for portability and to
remove the need for an interpreter. 

Currently this can load YAML files like the one provided to spawn a
process and write the stdout and stderr to output files. 

## Roadmap

* Hot-reload config on-demand
* Ability to actually manage processes
* Add HTTP RPC
* Improve console commands
* Support remote console
* Dashboard via HTTP 
