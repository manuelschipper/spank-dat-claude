.PHONY: all spank mcp-spank test clean

all: spank mcp-spank

spank:
	cd spank && go build -o spank .

mcp-spank:
	cd mcp-spank && go build -o mcp-spank .

test:
	cd spank && go test ./...
	cd mcp-spank && go test ./...
	cd vibe-check && python3 -m pytest .

clean:
	rm -f spank/spank mcp-spank/mcp-spank
