package pages

import (
	"context"

	"github.com/quantmind-br/model-loader/internal/service/hfhub"
	"github.com/quantmind-br/model-loader/internal/ui/components"
)

type hfSearcherAdapter struct{ client *hfhub.Client }

func (a hfSearcherAdapter) Search(ctx context.Context, query string, limit int) ([]components.SearchResult, error) {
	raw, err := a.client.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]components.SearchResult, len(raw))
	for i, r := range raw {
		out[i] = components.SearchResult(r)
	}
	return out, nil
}

type hfFileListerAdapter struct{ client *hfhub.Client }

func (a hfFileListerAdapter) RepoInfo(ctx context.Context, repoID string) (*components.RepoInfo, error) {
	info, err := a.client.RepoInfo(ctx, repoID)
	if err != nil {
		return nil, err
	}
	out := &components.RepoInfo{
		ID:       info.ID,
		Tags:     append([]string(nil), info.Tags...),
		Siblings: make([]components.Sibling, len(info.Siblings)),
	}
	for i, s := range info.Siblings {
		out.Siblings[i] = components.Sibling(s)
	}
	return out, nil
}
