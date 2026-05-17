// Gomavgen generates a Go package from a MAVLink dialect definition xml file and its includes.
package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"
)

// builtinTemplates holds the language templates shipped with gomavgen, so it
// can be run without a checkout of this repo. The base name without the .tmpl
// extension ("go", "h", "hh", "c_crc", "c_dec", "c_enc", "c_fmt") selects one.
//
//go:embed *.tmpl
var builtinTemplates embed.FS

// builtinNames returns the available builtin template names (without the .tmpl
// extension), sorted, for use in the usage message.
func builtinNames() []string {
	var names []string
	ents, _ := fs.Glob(builtinTemplates, "*.tmpl")
	for _, e := range ents {
		names = append(names, strings.TrimSuffix(e, ".tmpl"))
	}
	sort.Strings(names)
	return names
}

// loadTemplate resolves arg to a parsed template: first as the name of a
// builtin (e.g. "go"), and only if no such builtin exists, as a path to a
// template file on disk.
func loadTemplate(arg string) (*template.Template, error) {
	if data, err := builtinTemplates.ReadFile(arg + ".tmpl"); err == nil {
		log.Println("builtin template:", arg)
		return template.New(arg).Funcs(tmplfuncs).Parse(string(data))
	}
	log.Println("template file:", arg)
	return template.New(filepath.Base(arg)).Funcs(tmplfuncs).ParseFiles(arg)
}

func main() {

	log.SetFlags(0)
	log.SetPrefix("gomavgen: ")
	flag.Parse()

	if len(flag.Args()) != 2 {
		log.Fatalf("Usage: %s (%s | path/to/lang.tmpl) path/to/dialect.xml",
			os.Args[0], strings.Join(builtinNames(), " | "))
	}

	tmpl, err := loadTemplate(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}

	_, fname := filepath.Split(flag.Arg(1))
	basename := strings.ToLower(strings.TrimSuffix(fname, filepath.Ext(fname)))

	dialect := MAVLink{Name: basename}

	if err := loadDialect(flag.Arg(1), &dialect, map[string]*Enum{}, map[string]bool{}, 0); err != nil {
		log.Fatal(err)
	}

	log.Printf("Generating package %s dialect %d version %d", basename, dialect.Dialect, dialect.Version)

	// Mark fields whose enum the dialect declares as a bitmask, so templates
	// can annotate them accordingly.
	bitmask := map[string]bool{}
	for _, e := range dialect.Enums {
		if e.Bitmask {
			bitmask[e.Name] = true
		}
	}
	for _, m := range dialect.Messages {
		for _, f := range m.Fields {
			f.Bitmask = f.Enum != "" && bitmask[f.Enum]
		}
	}

	// fill in missing enum values, starting from highest found (?)
	for _, v := range dialect.Enums {
		max := uint64(0)
		for _, vv := range v.Entries {
			if vv.Value != "" {
				val, _ := strconv.ParseUint(vv.Value, 0, 32)
				if max < val {
					max = val
				}
			}
		}
		warnenums := false
		for i, vv := range v.Entries {
			if vv.Value == "" {
				if uint64(i) != max+1 {
					warnenums = true
				}
				vv.Value = fmt.Sprintf("%d", max+1)
				max++
			}
		}
		if warnenums {
			log.Printf("Possibly ill-defined mixing of explicit and implicit values in enum %s may be inconsistent", v.Name)
		}
	}

	sort.Sort(byMessageID(dialect.Messages))
	// stable reorder fields by their scalar size
	for _, v := range dialect.Messages {
		sort.Stable(bySerialisationOrder(v.Fields))
	}

	if err := tmpl.Execute(os.Stdout, dialect); err != nil {
		log.Fatal(err)
	}

}
