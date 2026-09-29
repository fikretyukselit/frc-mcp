/*
 * Synthetic test input for internal/ingest/source/cppheader (MIT, frc-mcp).
 * It imitates the constructs seen in vendor headers (export macros, nested
 * namespaces, [[deprecated]] overloads, requires-clauses, statement macros,
 * enum-like structs) without copying any vendor text.
 */
#pragma once

#include <string>
#include "acme/export.h"

#ifndef ACMEEXPORT
#define ACMEEXPORT
#endif

#define ACME_UNIT_ADD(name, output) \
    using name##_per_turn = output; \
    using name##_per_turn_t = output

namespace acme {
namespace motor {

class ParentDevice; // forward declaration: not a type definition

/**
 * \brief Common base of every device.
 *
 * \details More text that is not part of the brief.
 */
class ACMEEXPORT DeviceBase {
public:
    /** Refreshes all devices. Second sentence. */
    static int RefreshAll();
    virtual ~DeviceBase() = default;

protected:
    /** Hook for subclasses. */
    virtual void OnUpdate();

private:
    int m_secret;
    void Hidden();
};

namespace core {

/**
 * Generated device core.
 */
class CoreMotor : public DeviceBase {
public:
    CoreMotor(int id, std::string canbus = "");
    /// Gets the position.
    double GetPosition() const { return m_pos; }

    template <typename... Signals>
        requires (sizeof...(Signals) > 0) && std::is_same_v<int, int>
    static int WaitForAll(double timeout, Signals &...signals);

private:
    double m_pos{0.0};
};

} // namespace core

/**
 * \brief Motor controller.
 */
class Motor final : public core::CoreMotor, public other::Sendable<Motor>
{
public:
    /**
     * Constructs a motor.
     *
     * \param id  CAN id
     */
    explicit Motor(int id);

    /**
     * \deprecated Use the int constructor.
     */
    [[deprecated(
        "Constructing with a name is deprecated "
        "in 2027.")]]
    Motor(int id, std::string name);

    static Motor None() { return Motor{-1}; }

    void Set(double speed);
    Motor &operator=(Motor const &) = delete;
    bool operator==(Motor const &other) const;
    friend std::ostream &operator<<(std::ostream &os, Motor const &m);
    void Copy(Motor const &) = delete;

    static constexpr int kMaxId = 62;
    double speed = 0.0;

#if defined(_WIN32)
    void WindowsOnly();
#else
    void NotWindows();
#endif

#if 0
    class Broken {
#endif

protected:
    ACMEEXPORT void Protect(int x) override;
};

/**
 * \brief Neutral behavior (an enum-like struct).
 */
struct NeutralModeValue {
    int value;

    static constexpr int Coast = 0;
    static constexpr int Brake = 1;

    constexpr NeutralModeValue(int value) : value{value} {}
};

enum class Mode : int { kA = 0, kB = (1 << 2), kC };

/** Free function at namespace scope. */
ACMEEXPORT void FeedEnable(int timeoutMs);

inline constexpr double kVersion = 26.3;

using Speed = double;

ACME_UNIT_ADD(volts, double)
ACME_UNIT_ADD(amps, double)

namespace detail {
class Internal {};
}

namespace {
class Anonymous {};
}

} // namespace motor
} // namespace acme

namespace other {
template <typename T> class Sendable {};
}

template <>
struct std::hash<acme::motor::Motor> {
    size_t operator()(acme::motor::Motor const &m) const;
};
