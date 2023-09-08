module github.com/ianic/xnet/aio

go 1.21.0

require (
	github.com/pawelgaczynski/giouring v0.0.0-20230826085535-69588b89acb9
	github.com/stretchr/testify v1.8.4
	golang.org/x/sys v0.11.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

//replace github.com/pawelgaczynski/giouring => ../../giouring
