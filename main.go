// Gomavgen generates a Go package from a MAVLink dialect definition xml file and its includes.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"
)

func main() {

	log.SetFlags(0)
	log.SetPrefix("gomavgen: ")
	flag.Parse()

	if len(flag.Args()) != 2 {
		log.Fatalf("Usage: %s path/to/lang.tmpl path/to/dialect.xml", os.Args[0])
	}

	tmpl, err := template.New(filepath.Base(flag.Arg(0))).Funcs(tmplfuncs).ParseFiles(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}
	log.Println("template file:", tmpl.Name())

	_, fname := filepath.Split(flag.Arg(1))
	basename := strings.ToLower(strings.TrimSuffix(fname, filepath.Ext(fname)))

	dialect := MAVLink{Name: basename}

	if err := loadDialect(flag.Arg(1), &dialect, map[string]*Enum{}, map[string]bool{}, 0); err != nil {
		log.Fatal(err)
	}

	log.Printf("Generating package %s dialect %d version %d", basename, dialect.Dialect, dialect.Version)

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
