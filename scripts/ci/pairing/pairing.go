package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/google/go-github/v44/github"
	"golang.org/x/oauth2"
	"k8s.io/klog/v2"
)

type PullRequestMetadata struct {
	Author       string
	Organization string
	RepoName     string
	BranchName   string
	CommitSHA    string
	Number       int
	RemoteName   string
}

// listOpenPRs lists open pull requests for the given repository using the provided GitHub token.
func listOpenPRs(owner, repo string) ([]*github.PullRequest, error) {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: os.Getenv("GITHUB_TOKEN")})
	client := github.NewClient(oauth2.NewClient(context.Background(), ts))

	// list the open pull requests
	opt := &github.PullRequestListOptions{
		State: "open",
	}

	prs, _, err := client.PullRequests.List(context.Background(), owner, repo, opt)
	if err != nil {
		return nil, fmt.Errorf("could not list pull requests: %v", err)
	}

	return prs, nil
}

// shouldPair returns true if ...
func shouldPair(orgForPairing, repoForPairing, currentRemoteName, currentBranchName string) bool {
	pullRequests, err := listOpenPRs(orgForPairing, repoForPairing)
	if err != nil {
		log.Fatalf("Error listing pull requests: %v", err)
	}

	for _, pull := range pullRequests {
		fmt.Println("pull.GetHead().GetRef()", pull.GetHead().GetRef())
		fmt.Println("pull.GetUser().GetLogin()", pull.GetUser().GetLogin())

		if pull.GetHead().GetRef() == currentBranchName && pull.GetUser().GetLogin() == currentRemoteName {
			return true
		}
	}

	return false
}

// getCurrentPRInfo gets the current info of the PR that triggered the e2e CI prow job
func getCurrentPRInfo() (*PullRequestMetadata, error) {
	jobSpecEnvVarData := os.Getenv("JOB_SPEC")

	pr := &PullRequestMetadata{}

	if err := json.Unmarshal([]byte(jobSpecEnvVarData), pr); err != nil {
		return pr, fmt.Errorf("error when parsing openshift job spec data: %v", err)
	}

	return pr, nil
}

func clone(cloneDir, org, repo, prRemoteName, prBranchName string) {
	branch := "master"

	cloneDirInfo, err := os.Stat(cloneDir)
	if !os.IsNotExist(err) && cloneDirInfo.IsDir() {
		klog.Warningf("folder %s already exists... removing", cloneDir)

		err := os.RemoveAll(cloneDir)
		if err != nil {
			fmt.Printf("error removing %s folder", cloneDir)
		}
	}

	// if CI
	if prRemoteName != "" && prBranchName != "" {
		// check if pairing is required
		if shouldPair(org, repo, prRemoteName, prBranchName) {
			org = prRemoteName
			branch = prBranchName
			fmt.Println("should pair")
		}
	}

	url := fmt.Sprintf("https://github.com/%s/%s", org, repo)
	refName := fmt.Sprintf("refs/heads/%s", branch)

	log.Printf("cloning '%s' with git ref '%s'", url, refName)

	_, _ = git.PlainClone(cloneDir, false, &git.CloneOptions{
		URL:           url,
		ReferenceName: plumbing.ReferenceName(refName),
		Progress:      os.Stdout,
	})
}

func main() {
	HostOperatorCloneDir := os.Getenv("HOST_OPERATOR_CLONE_DIR")
	fmt.Println("HostOperatorCloneDir", HostOperatorCloneDir)
	MemberOperatorCloneDir := os.Getenv("MEMBER_OPERATOR_CLONE_DIR")
	RegistrationServiceCloneDir := os.Getenv("REGISTRATION_SERVICE_CLONE_DIR")
	KsctlCloneDir := os.Getenv("KSCTL_CLONE_DIR")
	prBranchName := ""
	prRemoteName := ""

	if os.Getenv("CI") == "true" {
		pr, err := getCurrentPRInfo()
		if err != nil {
			fmt.Println("err")
		}
		prBranchName = pr.BranchName
		prRemoteName = pr.RemoteName
	}

	clone(HostOperatorCloneDir, "codeready-toolchain", "host-operator", prBranchName, prRemoteName)
	clone(MemberOperatorCloneDir, "codeready-toolchain", "member-operator", prBranchName, prRemoteName)
	clone(RegistrationServiceCloneDir, "codeready-toolchain", "registration-service", prBranchName, prRemoteName)
	clone(KsctlCloneDir, "kubesaw", "ksctl", prBranchName, prRemoteName)
}
