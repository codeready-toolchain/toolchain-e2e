package main

type Pull struct {
	Author  string `json:"author"`
	Number  int    `json:"number"`
	HeadRef string `json:"head_ref"`
}

type Refs struct {
	Pulls []Pull `json:"pulls"`
}

type JobSpec struct {
	Refs Refs `json:"refs"`
}

type PullRequestMetadata struct {
	Author       string
	Organization string
	RepoName     string
	BranchName   string
	CommitSHA    string
	Number       string
	RemoteName   string
}
