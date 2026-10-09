package helpers

import (
	"errors"
	"testing"
)

const source = `/obj/effect/mapping_helpers/airlock
	name = "airlock helper"

/obj/effect/mapping_helpers/airlock/Initialize(mapload)
	. = ..()
	var/obj/machinery/door/airlock/airlock = locate(/obj/machinery/door/airlock) in loc
	if(!airlock)
		for(var/obj/machinery/door/window/windoor in loc)
			payload(windoor)
	// locate(/obj/item/never) in loc  <- a comment, not code

/obj/effect/mapping_helpers/apc/Initialize(mapload)
	var/obj/machinery/power/apc/target = locate(/obj/machinery/power/apc) in loc

/obj/effect/mapping_helpers/apc/cell_5k/payload(obj/machinery/power/apc/target)
	target.cell_type = /obj/item/stock_parts/power_store/battery/upgraded

/obj/effect/mapping_helpers/ianbirthday/Initialize(mapload)
	if(locate(/obj/structure/table/reinforced) in area_turf)
		return
`

func index() *Index {
	types := []Type{
		{Path: "/obj/effect/mapping_helpers/airlock", Abstract: true},
		{Path: "/obj/effect/mapping_helpers/airlock/access/all/engineering", Name: "engineering access", Desc: "Gives engineering access"},
		{Path: "/obj/effect/mapping_helpers/airlock/locked", Name: "locked"},
		{Path: "/obj/effect/mapping_helpers/apc/cell_5k", Name: "apc 5k cell"},
		{Path: "/obj/effect/mapping_helpers/ianbirthday", Name: "ian"},
	}
	return Build(types, []string{"helpers.dm", "helpers.dm", "missing.dm"}, func(f string) ([]byte, error) {
		if f == "helpers.dm" {
			return []byte(source), nil
		}
		return nil, errors.New("no file")
	})
}

func paths(hs []Helper) []string {
	var out []string
	for _, h := range hs {
		out = append(out, h.Path)
	}
	return out
}

func TestHelpersForObjectsTheirFamilyLooksUp(t *testing.T) {
	x := index()
	airlock := x.For("/obj/machinery/door/airlock/engineering")
	if got := paths(airlock); len(got) != 2 || got[0] != "/obj/effect/mapping_helpers/airlock/access/all/engineering" {
		t.Fatalf("airlock helpers = %v", got)
	}
	if airlock[0].Relative != "access/all/engineering" || airlock[0].Family != "/obj/effect/mapping_helpers/airlock" || airlock[0].Desc == "" {
		t.Fatalf("helper = %+v", airlock[0])
	}
	// The for-loop lookup: windoors take airlock helpers too.
	if got := paths(x.For("/obj/machinery/door/window/left")); len(got) != 2 {
		t.Fatalf("windoor helpers = %v", got)
	}
	if got := paths(x.For("/obj/machinery/power/apc/auto_name/directional/north")); len(got) != 1 || got[0] != "/obj/effect/mapping_helpers/apc/cell_5k" {
		t.Fatalf("apc helpers = %v", got)
	}
	// Lookups in other turfs, comments and abstract types do not count.
	if got := x.For("/obj/structure/table/reinforced"); len(got) != 0 {
		t.Fatalf("table helpers = %v", paths(got))
	}
	if got := x.For("/obj/item/never"); len(got) != 0 {
		t.Fatal("a commented lookup counted")
	}
}
