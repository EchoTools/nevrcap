package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/echotools/tape/v4/pkg/codec"
	"github.com/spf13/cobra"
)

func newStatsCommand() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "stats <file.tape>",
		Short: "Print aggregate per-player stats from a tape file",
		Long: `Read a .tape file and print aggregate statistics per player,
including goals, assists, saves, steals, stuns, blocks, shots, interceptions,
disc throws, catches, and approximate possession time.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStats(cmd, args[0], format)
		},
	}

	cmd.Flags().StringVar(&format, "format", "table", "output format: table|json")

	return cmd
}

// playerStats holds aggregate stats for a single player.
type playerStats struct {
	Name          string `json:"name"`
	Team          string `json:"team"`
	PossessionMs  uint32 `json:"possession_ms"`
	Slot          int32  `json:"slot"`
	Goals         int32  `json:"goals"`
	Assists       int32  `json:"assists"`
	Saves         int32  `json:"saves"`
	Steals        int32  `json:"steals"`
	Stuns         int32  `json:"stuns"`
	Blocks        int32  `json:"blocks"`
	Shots         int32  `json:"shots"`
	Interceptions int32  `json:"interceptions"`
	Throws        int32  `json:"throws"`
	Catches       int32  `json:"catches"`
}

func runStats(cmd *cobra.Command, filePath, format string) error {
	switch format {
	case "table", "json":
	default:
		return fmt.Errorf("unknown format: %s (use table or json)", format)
	}

	reader, err := codec.NewReader(filePath)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer reader.Close() //nolint:errcheck // best-effort cleanup

	header, err := reader.ReadHeader()
	if err != nil {
		return fmt.Errorf("read header: %w", err)
	}

	// Index players by slot. Seed from initial roster in the header.
	players := make(map[int32]*playerStats)
	if ea := header.GetEchoArena(); ea != nil {
		for _, pi := range ea.GetInitialRoster() {
			players[pi.GetSlot()] = &playerStats{
				Slot: pi.GetSlot(),
				Name: pi.GetDisplayName(),
				Team: roleToTeam(pi.GetRole()),
			}
		}
	}

	ensurePlayer := func(slot int32) *playerStats {
		ps, ok := players[slot]
		if !ok {
			ps = &playerStats{Slot: slot}
			players[slot] = ps
		}
		return ps
	}

	// Track possession holder across frames for approximate possession time.
	var prevHolder int32 = -1
	var prevTimestampMs uint32

	for {
		frame, readErr := reader.ReadFrame()
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return fmt.Errorf("read frame: %w", readErr)
		}

		ea := frame.GetEchoArena()
		if ea == nil {
			continue
		}

		tsMs := frame.GetTimestampOffsetMs()

		// Accumulate possession time from disc holder.
		// HasDiscHolderSlot distinguishes "slot 0 holds disc" from "no holder."
		if prevHolder >= 0 && tsMs > prevTimestampMs {
			ps := ensurePlayer(prevHolder)
			ps.PossessionMs += tsMs - prevTimestampMs
		}
		if ea.HasDiscHolderSlot() {
			prevHolder = ea.GetDiscHolderSlot()
		} else {
			prevHolder = -1
		}
		prevTimestampMs = tsMs

		// Process events.
		for _, evt := range ea.GetEvents() {
			switch e := evt.GetEvent().(type) {
			case *capturepb.EchoEvent_PlayerJoined:
				pj := e.PlayerJoined
				ps := ensurePlayer(pj.GetSlot())
				if ps.Name == "" {
					ps.Name = pj.GetDisplayName()
				}
				ps.Team = roleToTeam(pj.GetRole())

			case *capturepb.EchoEvent_PlayerSwitchedTeam:
				ps := ensurePlayer(e.PlayerSwitchedTeam.GetPlayerSlot())
				ps.Team = roleToTeam(e.PlayerSwitchedTeam.GetNewRole())

			case *capturepb.EchoEvent_PlayerGoal:
				ensurePlayer(e.PlayerGoal.GetPlayerSlot()).Goals++

			case *capturepb.EchoEvent_PlayerAssist:
				ensurePlayer(e.PlayerAssist.GetPlayerSlot()).Assists++

			case *capturepb.EchoEvent_PlayerSave:
				ensurePlayer(e.PlayerSave.GetPlayerSlot()).Saves++

			case *capturepb.EchoEvent_PlayerSteal:
				ensurePlayer(e.PlayerSteal.GetPlayerSlot()).Steals++

			case *capturepb.EchoEvent_PlayerStun:
				ensurePlayer(e.PlayerStun.GetPlayerSlot()).Stuns++

			case *capturepb.EchoEvent_PlayerBlock:
				ensurePlayer(e.PlayerBlock.GetPlayerSlot()).Blocks++

			case *capturepb.EchoEvent_PlayerShotTaken:
				ensurePlayer(e.PlayerShotTaken.GetPlayerSlot()).Shots++

			case *capturepb.EchoEvent_PlayerInterception:
				ensurePlayer(e.PlayerInterception.GetPlayerSlot()).Interceptions++

			case *capturepb.EchoEvent_DiscThrown:
				ensurePlayer(e.DiscThrown.GetPlayerSlot()).Throws++

			case *capturepb.EchoEvent_DiscCaught:
				ensurePlayer(e.DiscCaught.GetPlayerSlot()).Catches++
			}
		}
	}

	// Sort players by slot for deterministic output.
	slots := make([]int32, 0, len(players))
	for slot := range players {
		slots = append(slots, slot)
	}
	slices.Sort(slots)

	ordered := make([]*playerStats, 0, len(slots))
	for _, slot := range slots {
		ordered = append(ordered, players[slot])
	}

	out := cmd.OutOrStdout()

	switch format {
	case "json":
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(ordered)

	case "table":
		tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		printf(tw, "SLOT\tNAME\tTEAM\tG\tA\tSV\tST\tSTN\tBLK\tSHT\tINT\tTHR\tCTCH\tPOSS\n")
		printf(tw, "----\t----\t----\t-\t-\t--\t--\t---\t---\t---\t---\t---\t----\t----\n")
		for _, ps := range ordered {
			printf(tw, "%d\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%s\n",
				ps.Slot,
				truncate(ps.Name, 20),
				ps.Team,
				ps.Goals,
				ps.Assists,
				ps.Saves,
				ps.Steals,
				ps.Stuns,
				ps.Blocks,
				ps.Shots,
				ps.Interceptions,
				ps.Throws,
				ps.Catches,
				formatPossession(ps.PossessionMs),
			)
		}
		return tw.Flush()
	}

	return nil
}

func roleToTeam(r capturepb.Role) string {
	switch r {
	case capturepb.Role_ROLE_BLUE_TEAM:
		return "blue"
	case capturepb.Role_ROLE_ORANGE_TEAM:
		return "orange"
	case capturepb.Role_ROLE_SPECTATOR:
		return "spec"
	default:
		return ""
	}
}

func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "~"
}

func formatPossession(ms uint32) string {
	sec := ms / 1000
	frac := (ms % 1000) / 100
	min := sec / 60
	sec = sec % 60
	var b strings.Builder
	if min > 0 {
		fmt.Fprintf(&b, "%dm", min)
	}
	fmt.Fprintf(&b, "%d.%ds", sec, frac)
	return b.String()
}
