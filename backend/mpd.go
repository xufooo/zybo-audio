// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

var mpdAddr = "127.0.0.1:6600"

type mpdState struct {
	State          string
	PlaylistLength int
}

type mpdEntry struct {
	Name  string
	IsDir bool
	Size  int64
}

func mpdRequest(cmd string) ([]string, error) {
	conn, err := net.DialTimeout("tcp", mpdAddr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("mpd connect %s: %w", mpdAddr, err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(8 * time.Second))

	r := bufio.NewReader(conn)
	greet, err := r.ReadString('\n')
	if err != nil || !strings.HasPrefix(greet, "OK MPD ") {
		return nil, fmt.Errorf("bad mpd greeting: %q", strings.TrimSpace(greet))
	}
	fmt.Fprintf(conn, "%s\n", cmd)
	var out []string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("mpd read: %w", err)
		}
		line = strings.TrimSuffix(line, "\n")
		if line == "OK" {
			return out, nil
		}
		if strings.HasPrefix(line, "ACK ") {
			return nil, fmt.Errorf("mpd: %s", line)
		}
		out = append(out, line)
	}
}

func mpdKV(line string) (string, string) {
	k, v, _ := strings.Cut(line, ": ")
	return k, v
}

func mpdStatus() (mpdState, error) {
	lines, err := mpdRequest("status")
	if err != nil {
		return mpdState{}, err
	}
	st := mpdState{}
	for _, l := range lines {
		k, v := mpdKV(l)
		switch k {
		case "state":
			st.State = v
		case "playlistlength":
			fmt.Sscanf(v, "%d", &st.PlaylistLength)
		}
	}
	return st, nil
}

func mpdListFiles(uri string) ([]mpdEntry, error) {
	lines, err := mpdRequest(fmt.Sprintf("listfiles %s", strconv.Quote(uri)))
	if err != nil {
		return nil, err
	}
	var out []mpdEntry
	for _, l := range lines {
		k, v := mpdKV(l)
		switch k {
		case "file":
			out = append(out, mpdEntry{Name: v})
		case "directory":
			out = append(out, mpdEntry{Name: v, IsDir: true})
		case "size":
			if n := len(out); n > 0 && !out[n-1].IsDir {
				fmt.Sscanf(v, "%d", &out[n-1].Size)
			}
		}
	}
	return out, nil
}

type mpdSong struct {
	File   string
	Title  string
	Artist string
	Album  string
}

func mpdCurrentSong() (mpdSong, error) {
	lines, err := mpdRequest("currentsong")
	if err != nil {
		return mpdSong{}, err
	}
	var s mpdSong
	for _, l := range lines {
		k, v := mpdKV(l)
		switch k {
		case "file":
			s.File = v
		case "Title":
			s.Title = v
		case "Artist":
			s.Artist = v
		case "Album":
			s.Album = v
		}
	}

	if s.Title == "" {
		s.Title = s.File
	}
	return s, nil
}

func mpdEnqueue(fileOrURL string) error {
	st, err := mpdStatus()
	if err != nil {
		return err
	}
	if st.State == "stop" && st.PlaylistLength == 0 {
		return mpdPlayFile(fileOrURL)
	}
	_, err = mpdRequest(fmt.Sprintf("add %s", strconv.Quote(fileOrURL)))
	return err
}

func mpdWalkDir(dir string) ([]string, error) {
	var out []string
	var walk func(d string) error
	walk = func(d string) error {
		ents, err := mpdListFiles(d)
		if err != nil {
			return err
		}
		for _, e := range ents {
			p := e.Name
			if d != "" {
				p = d + "/" + e.Name
			}
			if e.IsDir {
				if err := walk(p); err != nil {
					return err
				}
				continue
			}
			out = append(out, p)
		}
		return nil
	}
	if err := walk(dir); err != nil {
		return nil, err
	}
	return out, nil
}

func mpdPlayFiles(files []string) error {
	if len(files) == 0 {
		return fmt.Errorf("nothing to play")
	}
	cmds := []string{"clear"}
	for _, f := range files {
		cmds = append(cmds, fmt.Sprintf("add %s", strconv.Quote(f)))
	}
	cmds = append(cmds, "play")
	for _, cmd := range cmds {
		if _, err := mpdRequest(cmd); err != nil {
			return err
		}
	}
	return nil
}

func mpdUpdate() (int, error) {
	lines, err := mpdRequest("update")
	if err != nil {
		return 0, err
	}
	n := 0
	for _, l := range lines {
		if k, v := mpdKV(l); k == "updating_db" {
			fmt.Sscanf(v, "%d", &n)
		}
	}
	return n, nil
}

func mpdPlayFile(file string) error {
	for _, cmd := range []string{"clear", fmt.Sprintf("add %s", strconv.Quote(file)), "play"} {
		if _, err := mpdRequest(cmd); err != nil {
			return err
		}
	}
	return nil
}

func mpdStop() error {
	_, err := mpdRequest("stop")
	return err
}

func mpdPause() error {
	_, err := mpdRequest("pause")
	return err
}

func mpdNext() error {
	_, err := mpdRequest("next")
	return err
}

func mpdPrev() error {
	_, err := mpdRequest("previous")
	return err
}

func mpdPlayID(id int) error {
	_, err := mpdRequest(fmt.Sprintf("playid %d", id))
	return err
}

type mpdQueueItem struct {
	Pos   int
	ID    int
	File  string
	Title string
}

type mpdQueueInfo struct {
	State   string
	Current int
	Items   []mpdQueueItem
}

func mpdQueue() (mpdQueueInfo, error) {
	out := mpdQueueInfo{Current: -1}
	lines, err := mpdRequest("playlistinfo")
	if err != nil {
		return out, err
	}
	cur := mpdQueueItem{Pos: -1}
	have := false
	flush := func() {
		if have {
			if cur.Title == "" {
				cur.Title = cur.File
			}
			out.Items = append(out.Items, cur)
		}
	}
	for _, l := range lines {
		k, v := mpdKV(l)
		switch k {
		case "file":

			flush()
			cur = mpdQueueItem{Pos: -1, File: v}
			have = true
		case "Pos":
			fmt.Sscanf(v, "%d", &cur.Pos)
		case "Title":
			cur.Title = v
		case "Id":
			fmt.Sscanf(v, "%d", &cur.ID)
		}
	}
	flush()
	st, err := mpdRequest("status")
	if err != nil {
		return out, err
	}
	for _, l := range st {
		k, v := mpdKV(l)
		switch k {
		case "state":
			out.State = v
		case "song":
			fmt.Sscanf(v, "%d", &out.Current)
		}
	}
	return out, nil
}

func quietMPD() error {
	var err error
	for i := 0; i < 10; i++ {
		if _, err = mpdRequest("stop"); err == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return err
}
