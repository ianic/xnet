module example/chat

go 1.21.0

require (
	github.com/ianic/ws v0.0.0-20230818145606-952328ae46cf
	github.com/klauspost/compress v1.16.7 // indirect
)

replace (
	github.com/ianic/ws => ../../
)
