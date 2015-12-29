![Trinity DB Logo](../gfx/trinity_m.png) 

# Trinity DB - Progress

## Overview

As of late Dec 2015, Trinity is pre-alpha. The basic node infrastructure is mostly in place, including the server binary, the TLS layer, the ability to listen for cluster connections and connect to other nodes. Nodes also notify connected nodes of new nodes, and swap consistent hash distributions and peer lists (CMD_DISTRIBUTION, CMD_PEERLIST), auto connnecting with new nodes supplied to them. There is also a Memcache interface that supports a couple of the basic protocol commands (set, get, delete). KV data is stored on the appropriate node using consistent hashing.

This represents a large amount of work in creative mode, and the next phase of the project will be to refactor, consolidate, tighten up and generally improve the code quality and structure. Also, I might write some actual tests :)

Next up, the KV data should replicate to the next 2 nodes on the circle. When a node joins, the other nodes should integrate its distribution into their circles in a 'syncing' (not readable) state and automatically copy the appropriate keys and values into the new node, calculating the new two recovery nodes and causing them to check that the appropriate keys and values exist, and sending delete notifications for each key to all other nodes.

Work continues on [bplustree](https://github.com/tomdionysus/bplustree) which will become the backend store for KV data.

**2015-12-18:** On a very valid suggestion from [@maetl](https://github.com/maetl) I've added snakeoil CA and peer certificates and quickstart [README](../README.md) to make spinning Trinity up for evaluation easier.  

## Completed

* Command Line flags
* TLS Layer Prototype
* GOB streaming between servers
* Heartbeats
* Basic Memcache interface
* Peer Swapping
* Autoconnect to all available nodes
* Integrate consistenthash
* Distribution now controls data storage location

## TODO

* Proxying GOBs, Data
* Replicating data to next two nodes

## BUGS

* In rare cicrumstances a node will be notified of its own address, and connect to itself.