![Trinity DB Logo](gfx/trinity_m.png) 

# Trinity DB

Trinity is a concept project for a relational database, designed from the ground up as a cloud system.

## Design goals

* Distributed Architecture - no masters/replicas/slaves, read/write to any node
* ANSI SQL92 compatible
* Built-in fast, distrubuted Key-Value Store
* Automatic replication and sharding
* Distributed Queries 
* Multi-mode consistency - per write, choose fire-and-forget, eventual, full
* Soft clusters - add/remove nodes at any time
* Capacity scales by adding nodes
* Optional Direct block level access, no filesystem
* All connections encrypted with TLS
* Zero configuration

## Language

Trinity is written in [Golang](https://golang.org).

## Progress

* Command Line flags
* TLS Layer Prototype

## TODO

* Autoconnect to all available nodes
* GOB streaming between servers
* Proxying GOBs, Data
* Integrate consistenthash
* Disk-based key/value store 
* Expose Key/value store