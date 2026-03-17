.PHONY: all spank test clean

all: spank

spank:
	cd spank && go build -o spank .

test:
	cd vibe-check && python3 -m pytest .

clean:
	rm -f spank/spank
