package sysinfo

import (
	"fmt"
	"strconv"
	"strings"
)

// disks reads usage for every mount point the sampler was given.
func (s *Sampler) disks() []Disk {
	if len(s.paths) == 0 {
		return nil
	}
	out := make([]Disk, 0, len(s.paths))
	for _, path := range s.paths {
		out = append(out, diskUsage(path))
	}
	return out
}

// diskUsage runs df for one mount point. "-P" asks for POSIX output, which
// guarantees one record per line: without it df wraps a long device name onto
// a second line and every field shifts. "-k" fixes the block size at 1024,
// since the default differs between Linux and the BSDs.
func diskUsage(path string) Disk {
	out, err := run("df", "-Pk", path)
	if err != nil {
		return Disk{Path: path}
	}
	d, err := parseDF(out)
	if err != nil {
		return Disk{Path: path}
	}
	d.Path = path
	return d
}

// parseDF reads the single data line of `df -Pk`:
//
//	Filesystem     1024-blocks      Used Available Capacity Mounted on
//	/dev/disk3s1s1   482797652  17631468  10144536      64%  /
//
// The three numbers are located by finding the first field that parses as
// one, rather than by counting from the left: a device name can contain a
// space ("map auto_home"), and the mount point at the right can too.
func parseDF(out string) (Disk, error) {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || strings.HasPrefix(line, "Filesystem") {
			continue
		}

		for i := 1; i+2 < len(fields); i++ {
			total, err1 := strconv.ParseInt(fields[i], 10, 64)
			used, err2 := strconv.ParseInt(fields[i+1], 10, 64)
			avail, err3 := strconv.ParseInt(fields[i+2], 10, 64)
			if err1 != nil || err2 != nil || err3 != nil || total <= 0 {
				continue
			}

			const kib = 1024
			return Disk{
				Used:  used * kib,
				Total: total * kib,
				Avail: avail * kib,
				OK:    true,
			}, nil
		}
	}
	return Disk{}, fmt.Errorf("df: no usable record")
}
