package pystub

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

const stub = `from __future__ import annotations
import typing
__all__: list[str] = ['XboxController', 'getTime']
class XboxController(GenericHID):
    """
    Handle input from Xbox controllers connected to the Driver Station.

    Members:

      kLeftBumper : left bumper.
    """
    class Button:
        """
        Represents a digital button on an XboxController.
        """
        kLeftBumper: typing.ClassVar[int] = 5
        _private: int
    def __init__(self, port: typing.SupportsInt) -> None:
        """
        Construct an instance of a controller.

        :param port: The port index on the Driver Station.
        """
    def getLeftBumper(self) -> bool:
        """
        Read the value of the left bumper.

        :deprecated: Use GetLeftBumperButton instead. This function is deprecated
                     for removal.
        """
    def getLeftBumperButton(self) -> bool:
        """
        Read the value of the left bumper button on the controller.
        """
    @typing.overload
    def setRumble(self, type: RumbleType, value: float) -> None:
        ...
    @property
    def port(self) -> int: ...
    def _hidden(self) -> None: ...
    @typing_extensions.deprecated("use newThing()")
    def oldThing(
        self,
        a: int,
        b: float = 1.0,
    ) -> None:
        pass
def getTime() -> float:
    """Gives real-time clock system time."""
class _Private:
    def x(self) -> None: ...
try:
    from wpilib import Sendable
    class Guarded(Sendable):
        def a(self) -> None: ...
except ImportError:
    class Guarded:
        def b(self) -> None: ...
`

func TestParseModule(t *testing.T) {
	ds := ParseModule(stub)
	byName := map[string]*Class{}
	var funcs []string
	for _, d := range ds {
		if d.Class != nil && byName[d.Class.Name] == nil { // first definition wins, as in Parse
			byName[d.Class.Name] = d.Class
		}
		if d.Func != nil {
			funcs = append(funcs, d.Func.Name)
		}
	}
	x := byName["XboxController"]
	if g := byName["Guarded"]; g == nil || g.Signature != "class Guarded(Sendable)" {
		t.Errorf("try-guarded class: %+v", g)
	}
	if x == nil || byName["XboxController.Button"] == nil || byName["_Private"] != nil || strings.Join(funcs, ",") != "getTime" {
		t.Fatalf("classes %v funcs %v", byName, funcs)
	}
	if x.Signature != "class XboxController(GenericHID)" || !strings.HasPrefix(firstSentence(x.Doc), "Handle input from Xbox controllers") {
		t.Errorf("class: %+v", x)
	}
	mem := map[string]Member{}
	for _, m := range x.Members {
		mem[m.Name] = m
	}
	if c := mem["XboxController"]; c.Kind != "constructor" || c.Signature != "def __init__(self, port: typing.SupportsInt) -> None" {
		t.Errorf("ctor %+v", c)
	}
	if d := mem["getLeftBumper"]; !strings.HasPrefix(d.Deprecated, "Use GetLeftBumperButton instead") || d.Use != "getLeftBumperButton" {
		t.Errorf("deprecated %+v", d)
	}
	if o := mem["oldThing"]; o.Deprecated != "use newThing()" || o.Signature != "def oldThing(self, a: int, b: float = 1.0,) -> None" {
		t.Errorf("multi-line/decorator %+v", o)
	}
	if p := mem["port"]; p.Kind != "property" {
		t.Errorf("property %+v", p)
	}
	if _, ok := mem["_hidden"]; ok {
		t.Error("private member kept")
	}
	if b := byName["XboxController.Button"]; len(b.Members) != 1 || b.Members[0].Name != "kLeftBumper" || b.Members[0].Kind != "field" {
		t.Errorf("nested %+v", b)
	}
}

func TestPublicModule(t *testing.T) {
	for in, want := range map[string]string{
		"wpilib/_wpilib/__init__":                                 "wpilib",
		"wpilib/drive/_drive":                                     "wpilib.drive",
		"wpimath/_controls/_controls/trajectory":                  "wpimath.trajectory",
		"commands2/button/trigger":                                "commands2.button.trigger",
		"_private/x":                                              "x",
		"phoenix6/hardware/talon_fx":                              "phoenix6.hardware.talon_fx",
		"phoenix6-26.3.0.data/purelib/phoenix6/hardware/talon_fx": "phoenix6.hardware.talon_fx",
	} {
		if got := PublicModule(in); got != want {
			t.Errorf("%s: %q want %q", in, got, want)
		}
	}
}

func FuzzParseModule(f *testing.F) {
	f.Add(stub)
	f.Add("class A:\n    def f(\n")
	f.Fuzz(func(t *testing.T, s string) { _ = ParseModule(s) })
}

func TestParseWheelReexports(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.whl")
	fh, _ := os.Create(p)
	zw := zip.NewWriter(fh)
	for name, body := range map[string]string{
		"phoenix6-26.3.0.data/purelib/phoenix6/__init__.py":          "from .status_code import StatusCode\n",
		"phoenix6-26.3.0.data/purelib/phoenix6/hardware/__init__.py": "from .talon_fx import TalonFX\nfrom .cancoder import (\n    CANcoder,\n)\n",
		"phoenix6-26.3.0.data/purelib/phoenix6/hardware/talon_fx.py": "class TalonFX(CoreTalonFX):\n    \"\"\"Class description for the Talon FX integrated motor controller.\"\"\"\n    def set_control(self, request) -> StatusCode:\n        return 0\n",
		"phoenix6-26.3.0.data/purelib/phoenix6/hardware/cancoder.py": "class CANcoder:\n    def get_position(self) -> float: ...\n",
		"phoenix6-26.3.0.data/purelib/phoenix6/status_code.py":       "class StatusCode:\n    OK: int\n",
		"phoenix6-26.3.0.dist-info/METADATA":                         "x",
	} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := fh.Close(); err != nil {
		t.Fatal(err)
	}
	src := sources.Source{Library: "phoenix6", Season: "2026", Channel: "stable", Version: "26.3.0", License: "X", Trust: "vendor", BaseURL: "https://x/"}
	got := map[string]bool{}
	if _, err := Parse(p, src, "r", time.Unix(0, 0), func(s index.Symbol) error { got[s.FQN] = true; return nil }, func(index.Chunk) error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"phoenix6.hardware.TalonFX", "phoenix6.hardware.TalonFX#set_control", "phoenix6.hardware.CANcoder#get_position", "phoenix6.StatusCode"} {
		if !got[want] {
			t.Errorf("missing %s in %v", want, got)
		}
	}
}
