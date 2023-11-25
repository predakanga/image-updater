package pkg

import (
	"bytes"
	"context"
	"fmt"
	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
	"sync"
)

type Repository struct {
	Mutex       sync.Mutex
	url         string
	branch      string
	commitName  string
	commitEmail string
	username    string
	password    string
	storage     *memory.Storage
	filesystem  billy.Filesystem
	repository  *git.Repository
}

func NewRepository(cfg RepositoryConfig) *Repository {
	return &Repository{
		url:         cfg.Url,
		branch:      cfg.Branch,
		commitName:  cfg.CommitterName,
		commitEmail: cfg.CommitterEmail,
		username:    cfg.Username,
		password:    cfg.Password,
		storage:     nil,
		filesystem:  nil,
	}
}

func (r *Repository) Discard() {
	r.storage = nil
	r.filesystem = nil
}

func (r *Repository) Fetch(ctx context.Context) error {
	// Create a fresh set of storage
	r.storage = memory.NewStorage()
	r.filesystem = memfs.New()

	// Actually perform the fetch
	if repo, err := git.CloneContext(ctx, r.storage, r.filesystem, &git.CloneOptions{
		URL: r.url,
		Auth: &http.BasicAuth{
			Username: r.username,
			Password: r.password,
		},
		ReferenceName: plumbing.NewBranchReferenceName(r.branch),
		SingleBranch:  true,
		Tags:          git.NoTags,
	}); err == nil {
		r.repository = repo
	} else {
		return err
	}

	// Configure the committer details
	if cfg, err := r.repository.Config(); err != nil {
		return fmt.Errorf("configuring repository failed: %w", err)
	} else {
		cfg.Author.Name = r.commitName
		cfg.Author.Email = r.commitEmail
		if err := r.repository.SetConfig(cfg); err != nil {
			return fmt.Errorf("configuring repository failed: %w", err)
		}
	}

	return nil
}

func (r *Repository) Worktree() (*git.Worktree, error) {
	return r.repository.Worktree()
}

func (r *Repository) Push(ctx context.Context) error {
	buf := bytes.Buffer{}
	err := r.repository.PushContext(ctx, &git.PushOptions{
		Auth: &http.BasicAuth{
			Username: r.username,
			Password: r.password,
		},
		Progress: &buf,
	})
	if err != nil {
		return fmt.Errorf("push failed: %w\ndetails: %v", err, buf.String())
	}

	return nil
}
