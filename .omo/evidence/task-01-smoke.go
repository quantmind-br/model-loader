package main

import (
	"fmt"
	hfhub "github.com/quantmind-br/model-loader/internal/service/hfhub"
)

func main() {
	client := hfhub.NewClient(nil, "model-loader/test")
	fmt.Printf("client created: %T\n", client)

	res := hfhub.SearchResult{
		ID:     "TheBloke/Llama-2-7B-GGUF",
		Tags:   []string{"gguf", "llama"},
		Likes:  42,
		ModelID: "Llama-2-7B-GGUF",
	}
	fmt.Printf("search result: ID=%s HasGGUF=%v\n", res.ID, res.HasGGUFTag())

	info := hfhub.RepoInfo{ID: "foo/bar"}
	fmt.Printf("repo info: %s\n", info.ID)

	err := hfhub.ErrHTTP{Status: 404, URL: "https://huggingface.co/api/models/foo/bar"}
	fmt.Printf("error: %s\n", err.Error())
}
