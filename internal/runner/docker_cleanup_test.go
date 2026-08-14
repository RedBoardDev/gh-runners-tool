package runner

import (
	"slices"
	"testing"
)

func TestSelectJobContainers(t *testing.T) {
	workdir := "/var/lib/ghr/runners/kare-qa/kare-qa-abc123"
	containers := []containerInfo{
		// Job container: mounts the workdir, on a job network.
		{id: "job1", mounts: []string{workdir + "/_work/_temp"}, networks: []string{"github_network_aaa"}},
		// Service containers: no workdir mount, same job network.
		{id: "svc1", networks: []string{"github_network_aaa"}},
		{id: "svc2", networks: []string{"github_network_aaa"}},
		// Another runner's job: different workdir, different network.
		{id: "other", mounts: []string{"/var/lib/ghr/runners/kare-qa/kare-qa-zzz999/_work"}, networks: []string{"github_network_bbb"}},
		// Unrelated container (e.g. a preview deploy) on the default bridge.
		{id: "preview", networks: []string{"bridge"}},
	}

	ids, networks := selectJobContainers(containers, workdir)

	slices.Sort(ids)
	if want := []string{"job1", "svc1", "svc2"}; !slices.Equal(ids, want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	if want := []string{"github_network_aaa"}; !slices.Equal(networks, want) {
		t.Fatalf("networks = %v, want %v", networks, want)
	}
}

func TestSelectJobContainersNoMatch(t *testing.T) {
	containers := []containerInfo{
		{id: "preview", networks: []string{"bridge"}},
	}
	ids, networks := selectJobContainers(containers, "/var/lib/ghr/runners/kare-qa/kare-qa-abc123")
	if len(ids) != 0 || len(networks) != 0 {
		t.Fatalf("expected no matches, got ids=%v networks=%v", ids, networks)
	}
}

func TestParseContainerLines(t *testing.T) {
	out := "id1|/a,/b,|github_network_x,\nid2||bridge,\n\n"
	containers := parseContainerLines(out)
	if len(containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(containers))
	}
	if !slices.Equal(containers[0].mounts, []string{"/a", "/b"}) {
		t.Fatalf("mounts = %v", containers[0].mounts)
	}
	if !slices.Equal(containers[1].networks, []string{"bridge"}) {
		t.Fatalf("networks = %v", containers[1].networks)
	}
}
