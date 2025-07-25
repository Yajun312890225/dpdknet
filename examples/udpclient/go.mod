module udpclient

go 1.24.3

require github.com/Yajun312890225/dpdknet v0.0.0

require (
	github.com/Yajun312890225/nff-go v0.0.0-20250721065636-ec27cf0d3b42 // indirect
	github.com/google/btree v1.1.2 // indirect
	github.com/google/gopacket v1.1.19 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	golang.org/x/sys v0.34.0 // indirect
	golang.org/x/time v0.7.0 // indirect
	gvisor.dev/gvisor v0.0.0-20250205023644-9414b50a5633 // indirect
)

replace github.com/Yajun312890225/dpdknet => ../../

replace github.com/Yajun312890225/nff-go => ../../../nff-go
