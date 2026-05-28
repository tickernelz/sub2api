package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type updateTestCache struct{}

func (updateTestCache) GetUpdateInfo(context.Context) (string, error) {
	return "", errors.New("cache miss")
}

func (updateTestCache) SetUpdateInfo(context.Context, string, time.Duration) error {
	return nil
}

type recordingGitHubReleaseClient struct {
	repo           string
	downloadCalled bool
}

func (c *recordingGitHubReleaseClient) FetchLatestRelease(_ context.Context, repo string) (*GitHubRelease, error) {
	c.repo = repo
	return &GitHubRelease{
		TagName:     "v0.1.140",
		Name:        "Sub2API 0.1.140",
		Body:        "release notes",
		PublishedAt: "2026-05-28T00:00:00Z",
		HTMLURL:     "https://github.com/" + repo + "/releases/tag/v0.1.140",
	}, nil
}

func (c *recordingGitHubReleaseClient) DownloadFile(context.Context, string, string, int64) error {
	c.downloadCalled = true
	return nil
}

func (c *recordingGitHubReleaseClient) FetchChecksumFile(context.Context, string) ([]byte, error) {
	return nil, nil
}

func TestUpdateServiceChecksForkReleaseRepository(t *testing.T) {
	client := &recordingGitHubReleaseClient{}
	svc := NewUpdateService(updateTestCache{}, client, "0.1.139", "release")

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.Equal(t, "tickernelz/sub2api", client.repo)
	require.Equal(t, "0.1.140", info.LatestVersion)
	require.True(t, info.HasUpdate)
	require.NotNil(t, info.ReleaseInfo)
	require.Equal(t, "https://github.com/tickernelz/sub2api/releases/tag/v0.1.140", info.ReleaseInfo.HTMLURL)
}

func TestUpdateServiceReportsDockerBuildTypeFromDeploymentEnv(t *testing.T) {
	t.Setenv("SUB2API_DEPLOYMENT", "docker")
	client := &recordingGitHubReleaseClient{}
	svc := NewUpdateService(updateTestCache{}, client, "0.1.139", "release")

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.Equal(t, "docker", info.BuildType)
}

func TestPerformUpdateRejectsDockerDeployment(t *testing.T) {
	t.Setenv("SUB2API_DEPLOYMENT", "docker")
	client := &recordingGitHubReleaseClient{}
	svc := NewUpdateService(updateTestCache{}, client, "0.1.139", "release")

	err := svc.PerformUpdate(context.Background())

	require.ErrorContains(t, err, "docker deployments must be updated by pulling the Docker image")
	require.False(t, client.downloadCalled)
}
