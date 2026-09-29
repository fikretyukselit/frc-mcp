package verify

import (
	"slices"
	"testing"
)

func TestSupertypesKeepsEveryInterface(t *testing.T) {
	for sig, want := range map[string][]string{
		"public final class Pose2d extends Object implements Interpolatable<Pose2d>, ProtobufSerializable, StructSerializable": {"Object", "Interpolatable", "ProtobufSerializable", "StructSerializable"},
		"public class TalonFX extends CoreTalonFX implements AutoCloseable":                                                    {"CoreTalonFX", "AutoCloseable"},
		"public interface Subsystem extends Sendable, Comparable<Subsystem>":                                                   {"Sendable", "Comparable"},
		"public class A<T extends Number> extends B<Map<K, V>> implements x.y.C {":                                             {"B", "C"},
	} {
		if got := supertypes(sig); !slices.Equal(got, want) {
			t.Errorf("supertypes(%q) = %v, want %v", sig, got, want)
		}
	}
}

func TestCallArgsGenerics(t *testing.T) {
	src := stripJava(`f(new HashMap<Object, Command>(), () -> 1, a < b, c.<String, Integer>of(x, y))`)
	args, ok := callArgs(src, 2)
	if !ok || len(args) != 4 {
		t.Fatalf("args = %q", args)
	}
}

func TestDeclaredVarsAmbiguousAndNested(t *testing.T) {
	imported := map[string]string{"Variable": "org.wpilib.math.autodiff.Variable", "Pose2d": "org.wpilib.math.geometry.Pose2d",
		"TrapezoidProfile": "org.wpilib.math.trajectory.TrapezoidProfile"}
	v := declaredVars("class A { Variable x; TrapezoidProfile.Constraints c; Pose2d p; }\nclass B { Pose2d x; }", imported)
	if _, ok := v["x"]; ok {
		t.Errorf("x is declared with two types and must be left out: %v", v)
	}
	if v["c"] != "org.wpilib.math.trajectory.TrapezoidProfile.Constraints" || v["p"] != "org.wpilib.math.geometry.Pose2d" {
		t.Errorf("vars = %v", v)
	}
}
