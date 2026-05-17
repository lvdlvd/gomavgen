package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
)

// maxIncludeDepth bounds the include recursion to guard against cycles and
// pathologically deep include chains.
const maxIncludeDepth = 5

// loadDialect parses the MAVLink xml file at path and recursively parses its
// includes (which may themselves contain includes), merging everything into
// dialect. enums tracks enums already merged, keyed by name, so that repeated
// declarations across files have their entries concatenated rather than
// duplicated. seen tracks the cleaned absolute path of every file already
// loaded, so that diamond includes and cycles each load a file at most once.
// depth counts include nesting and starts at 0 for the top-level file.
//
// The MAVLink spec says included enum declarations come first; we satisfy this
// by recursing into includes before merging the current file's own enums and
// messages. Version and dialect are taken from the outermost file that
// specifies a non-zero value (top-level wins, then includes in order).
func loadDialect(path string, dialect *MAVLink, enums map[string]*Enum, seen map[string]bool, depth int) error {
	if depth > maxIncludeDepth {
		return fmt.Errorf("%s: include depth exceeds %d", path, maxIncludeDepth)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if seen[abs] {
		log.Printf("Already loaded %s, skipping", path)
		return nil
	}
	seen[abs] = true

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	var m MAVLink
	err = xml.NewDecoder(f).Decode(&m)
	f.Close()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	log.Printf("Loaded %s dialect %d version %d (depth %d)", path, m.Dialect, m.Version, depth)

	if dialect.Version == 0 {
		dialect.Version = m.Version
	}
	if dialect.Dialect == 0 {
		dialect.Dialect = m.Dialect
	}

	// Recurse into includes first so that included declarations come before
	// the ones in this file, as the spec requires.
	dname := filepath.Dir(path)
	for _, inc := range m.Include {
		if err := loadDialect(filepath.Join(dname, inc), dialect, enums, seen, depth+1); err != nil {
			return err
		}
	}

	// Enum declarations are merged: a repeated enum has the entries of this
	// (later) file appended after the ones already collected from includes.
	// The semantics of repeated enum entries or repeated message declarations
	// are left open by the spec, so we naively concatenate and leave it to the
	// Go compiler to flag redefinitions.
	for _, e := range m.Enums {
		if enums[e.Name] == nil {
			enums[e.Name] = e
			dialect.Enums = append(dialect.Enums, e)
			continue
		}
		log.Printf("Merging %s enum %q", path, e.Name)
		enums[e.Name].Entries = append(enums[e.Name].Entries, e.Entries...)
	}

	dialect.Messages = append(dialect.Messages, m.Messages...)
	return nil
}

// The structure of the MAV schema
//
//	https://github.com/ArduPilot/pymavlink/blob/master/generator/mavschema.xsd
type MAVLink struct {
	Name     string
	Include  []string   `xml:"include"`
	Version  uint8      `xml:"version"`
	Dialect  uint8      `xml:"dialect"`
	Enums    []*Enum    `xml:"enums>enum"`
	Messages []*Message `xml:"messages>message"`
}

type Enum struct {
	Name        string   `xml:"name,attr"`
	Bitmask     bool     `xml:"bitmask,attr"`
	Description string   `xml:"description"`
	Entries     []*Entry `xml:"entry"`
}

// Note: Entry uses Value type string instead of uint32 to accept non-decimal enum>entry>values,
// see https://github.com/ArduPilot/pymavlink/blob/master/generator/mavschema.xsd#L19

type Entry struct {
	Value       string   `xml:"value,attr"` // See note
	Name        string   `xml:"name,attr"`
	Description string   `xml:"description"`
	Params      []*Param `xml:"param"`
}

type Param struct {
	Index         uint8   `xml:"index,attr"`
	Description   string  `xml:",innerxml"`
	Label         string  `xml:"label,attr"`
	Units         string  `xml:"units,attr"`
	Enum          string  `xml:"enum,attr"`
	DecimalPlaces uint8   `xml:"decimalPlaces,attr"`
	Increment     float32 `xml:"increment,attr"`
	MinValue      float32 `xml:"minValue,attr"`
	MaxValue      float32 `xml:"maxValue,attr"`
	Reserved      bool    `xml:"reserved,attr"`
}

type Message struct {
	ID          uint32   `xml:"id,attr"`
	Name        string   `xml:"name,attr"`
	Description string   `xml:"description"`
	Fields      []*Field `xml:"field"`
}

type Field struct {
	CType       string `xml:"type,attr"`
	Name        string `xml:"name,attr"`
	Enum        string `xml:"enum,attr"`
	Description string `xml:",innerxml"`
	IsExtension bool
	// Bitmask is set after parsing when Enum refers to an enum the dialect
	// declares as a bitmask. It is not read from the xml.
	Bitmask bool
}

// Need to unmarshal Message by hand because '<extensions/>' changes the value of an attribute of nested tag 'field'
func (m *Message) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for _, v := range start.Attr {
		switch v.Name.Local {
		case "id":
			vv, err := strconv.ParseUint(v.Value, 0, 32)
			if err != nil {
				return err
			}
			m.ID = uint32(vv)
		case "name":
			m.Name = v.Value
		}
	}
	var ext bool
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch tok := token.(type) {
		case xml.StartElement:
			switch tok.Name.Local {
			case "field":
				f := &Field{IsExtension: ext}
				if err := d.DecodeElement(f, &tok); err != nil {
					return err
				}
				m.Fields = append(m.Fields, f)
			case "extensions":
				ext = true
			case "description":
				if err := d.DecodeElement(&m.Description, &tok); err != nil {
					return err
				}
			}
		}

	}
	return nil
}

func (m *Message) BaseFields() []*Field {
	var r []*Field
	for _, v := range m.Fields {
		if !v.IsExtension {
			r = append(r, v)
		}
	}
	return r
}
func (m *Message) ExtFields() []*Field {
	var r []*Field
	for _, v := range m.Fields {
		if v.IsExtension {
			r = append(r, v)
		}
	}
	return r
}

type byMessageID []*Message

func (s byMessageID) Len() int           { return len(s) }
func (s byMessageID) Less(i, j int) bool { return s[i].ID < s[j].ID }
func (s byMessageID) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

type bySerialisationOrder []*Field

func (s bySerialisationOrder) Len() int { return len(s) }
func (s bySerialisationOrder) Less(i, j int) bool {
	if s[i].IsExtension != s[j].IsExtension {
		return !s[i].IsExtension
	}
	return scalarSize(s[i].CType) > scalarSize(s[j].CType)
}                                            // reverse!
func (s bySerialisationOrder) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

// lifted from
// https://github.com/mavlink/c_library_v2/blob/master/checksum.h#L25
// https://play.golang.org/p/ycYYW7bMChP
type crc16x25 uint16

func (acc *crc16x25) Update(b []byte) uint16 {
	for _, v := range b {
		t := v ^ byte(*acc)
		t ^= t << 4
		u := uint16(t)
		*acc = crc16x25(uint16(*acc)>>8 ^ u<<8 ^ u<<3 ^ u>>4)
	}
	return uint16(*acc)
}

func (m *Message) CRCExtra() byte {
	x := crc16x25(0xffff)
	x.Update([]byte(m.Name))
	x.Update([]byte(" "))
	for _, v := range m.Fields {
		if v.IsExtension {
			break
		}
		parts := reCType.FindStringSubmatch(v.CType)
		if len(parts) != 3 {
			log.Fatalf("Can't parse message %q field %q as ctype", m.Name, v.CType)
		}
		// srsly guys.
		if parts[1] == "uint8_t_mavlink_version" {
			parts[1] = "uint8_t"
		}

		x.Update([]byte(parts[1])) // i suspect that the uint8_t_mavlink_version field in Heartbeat  messes things up
		x.Update([]byte(" "))
		x.Update([]byte(v.Name))
		x.Update([]byte(" "))
		if parts[2] != "" {
			n, err := strconv.ParseUint(parts[2][1:len(parts[2])-1], 10, 8)
			if err != nil {
				log.Fatalf("Can't parse message %q field %q as ctype, invalid array length:%v", m.Name, v.CType, err)
			}
			x.Update([]byte{byte(n)})
		}
	}
	return byte(x) ^ byte(x>>8)
}
