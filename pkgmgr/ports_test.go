package pkgmgr

import (
	"net"
	"strconv"
	"testing"
)

func TestReservedNativePortsAcrossContexts(t *testing.T) {
	registry := PortRegistry{
		"context-a": {
			"same-package": {
				nativePortService: {"api": "41001"},
				"docker":          {"8080": "41004"},
			},
			"other-package": {
				nativePortService: {"api": "41002"},
				"docker":          {"8080": "41005"},
			},
		},
		"context-b": {
			"same-package": {
				nativePortService: {"api": "41003"},
			},
		},
	}

	got := reservedNativePorts(registry, "context-a", "same-package")
	for _, port := range []string{"41002", "41003", "41004", "41005"} {
		if _, ok := got[port]; !ok {
			t.Errorf("expected saved native port %s to be reserved", port)
		}
	}
	if _, ok := got["41001"]; ok {
		t.Fatal("current package's saved port should be eligible for reuse")
	}
}

func TestAllocateNativePortsAvoidReservedAndUnavailablePorts(t *testing.T) {
	occupied, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	occupiedPort := strconv.Itoa(occupied.Addr().(*net.TCPAddr).Port)

	pkg := Package{Ports: []string{"api"}}
	registered := PackagePortRegistry{
		nativePortService: {"api": occupiedPort},
	}
	ports, reservations, err := pkg.allocatePorts(registered, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, listener := range reservations {
			_ = listener.Close()
		}
	}()

	port := ports[nativePortService]["api"]
	if port == occupiedPort {
		t.Fatalf("reused unavailable native port %s", port)
	}
	if _, err := net.Listen("tcp", net.JoinHostPort("", port)); err == nil {
		t.Fatalf("allocated native port %s was not held until service startup", port)
	}
}

func TestAllocateNativePortsAvoidsOtherContextAssignments(t *testing.T) {
	first, firstReservations, err := (Package{Ports: []string{"api"}}).allocatePorts(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, listener := range firstReservations {
			_ = listener.Close()
		}
	}()
	firstPort := first[nativePortService]["api"]

	registry := PortRegistry{
		"context-a": {"pkg": first},
	}
	reserved := reservedNativePorts(registry, "context-b", "pkg")
	second, secondReservations, err := (Package{Ports: []string{"api"}}).allocatePorts(nil, reserved)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, listener := range secondReservations {
			_ = listener.Close()
		}
	}()
	if got := second[nativePortService]["api"]; got == firstPort {
		t.Fatalf("contexts received the same native port %s", got)
	}
}

func TestValidateRejectsDockerContainerNamedNative(t *testing.T) {
	pkg := Package{
		Name:     "pkg",
		Version:  "1.0.0",
		filePath: "pkg/pkg-1.0.0.yaml",
		Ports:    []string{"api"},
		InstallSteps: []PackageInstallStep{
			{Docker: &PackageInstallStepDocker{ContainerName: nativePortService}},
		},
	}
	if err := pkg.validate(Config{Template: NewTemplate(nil)}); err == nil {
		t.Fatal("expected native Docker container name to be rejected")
	}
}
