package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"docullm/internal/docparse"
	"docullm/internal/mcpserver"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"
)

func printCorpus(c *docparse.Corpus, asJSON bool) {
	var enc []byte
	var err error
	if asJSON {
		enc, err = json.MarshalIndent(c, "", "  ")
	} else {
		enc, err = yaml.Marshal(c)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error encoding output:", err)
		os.Exit(1)
	}
	fmt.Print(string(enc))
}

func main() {
	file := flag.String("f", "", "markdown file to parse, may also be given as argument (default: stdin)")
	tree := flag.Bool("tree", false, "dump the raw markdown node tree as json")
	asJSON := flag.Bool("json", false, "print documents as json instead of yaml")
	dir := flag.String("d", "", "directory of markdown files to read instead of a single file")
	serve := flag.Bool("mcp", false, "serve the documents as MCP server on stdin/stdout")
	flag.Parse()
	if *file == "" && flag.NArg() > 0 {
		*file = flag.Arg(0)
	}

	if *serve || *dir != "" {
		root := *dir
		if root == "" {
			root = *file
		}
		if root == "" {
			fmt.Fprintln(os.Stderr, "-mcp needs a file or a directory (-d)")
			os.Exit(1)
		}
		corpus, err := docparse.LoadCorpus(root)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error reading input:", err)
			os.Exit(1)
		}
		if *serve {
			if err := mcpserver.NewServer(corpus).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
				fmt.Fprintln(os.Stderr, "mcp server:", err)
				os.Exit(1)
			}
			return
		}
		printCorpus(corpus, *asJSON)
		return
	}

	var data []byte
	var err error
	if *file != "" {
		data, err = os.ReadFile(*file)
	} else {
		data, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error reading input:", err)
		os.Exit(1)
	}

	if !*tree {
		name := *file
		if name == "" {
			name = "stdin"
		}
		printCorpus(docparse.BuildDocuments(docparse.SplitSources(data, name)), *asJSON)
		return
	}

	out := docparse.BuildTree(data)

	enc, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error encoding json:", err)
		os.Exit(1)
	}
	fmt.Println(string(enc))
}
