package main

import (
	"github.com/athxx/dax"
)

/*
why use Unix sockets?
1. Performance: Unix sockets can be faster than TCP/IP sockets for local communication because they avoid the overhead of the network stack. 20~30% performance improvement is common.
2. Security: Unix sockets can have file system permissions, allowing you to restrict access to the socket file, which can enhance security.
3. Simplicity: For applications that only need to communicate within the same host, using Unix sockets can simplify the architecture and reduce potential points of failure.

# nginx configuration example:

server {
	listen 80;
	server_name example.com;
	location / {
		proxy_pass http://unix:/tmp/dax.sock:/;
		proxy_set_header Host $host;
		proxy_set_header X-Real-IP $remote_addr;
	}
}

# test

curl -s --unix-socket /tmp/dax.sock http://localhost/

curl -s --unix-socket /tmp/dax.sock http://localhost/hi

*/

func main() {
	socketPath := "/tmp/dax.sock"

	s := dax.NewServer(&dax.Config{
		EnableBootMsg: true,
		EnablePrefork: true,
	})
	s.Get("/", func(ctx dax.Context) error {
		return ctx.String("Hello from Unix socket!")
	})

	s.Get("/hi", func(ctx dax.Context) error {
		return ctx.String("Hi! Here is a Unix socket example.")
	})

	// Listen on the pre-created Unix socket
	s.RunUnix(socketPath)
}
