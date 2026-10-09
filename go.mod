module github.com/resoltico/pdfconcat

go 1.27.2

require (
	github.com/benoitkugler/pdf v0.0.15
	github.com/benoitkugler/pstokenizer v1.0.1
	github.com/go-text/typesetting v0.3.5
	github.com/pdfcpu/pdfcpu v0.16.1
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3
	go.yaml.in/yaml/v3 v3.0.5
	golang.org/x/image v0.46.0
	golang.org/x/sys v0.48.0
	golang.org/x/term v0.46.0
	golang.org/x/text v0.42.0
)

require (
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/hhrutter/lzw v1.0.0 // indirect
	github.com/hhrutter/tiff v1.0.7 // indirect
	github.com/mattn/go-runewidth v0.0.30 // indirect
	golang.org/x/crypto v0.57.0 // indirect
)

replace github.com/benoitkugler/pdf => ./third_party/benoitkugler/pdf

replace github.com/benoitkugler/pstokenizer => ./third_party/benoitkugler/pstokenizer

replace github.com/pdfcpu/pdfcpu => ./third_party/pdfcpu/pdfcpu
