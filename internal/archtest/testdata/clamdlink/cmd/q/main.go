// Command q dials TCP for something that is not clamd: not named.
package main

import "net"

func main() { _, _ = net.Dial("tcp", "127.0.0.1:8080") }
