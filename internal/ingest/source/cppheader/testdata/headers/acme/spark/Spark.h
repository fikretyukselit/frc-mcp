// Synthetic test input for internal/ingest/source/cppheader (MIT, frc-mcp).
#pragma once

#ifdef __GNUC__
#pragma GCC diagnostic push
#endif

#include <cstdint>

namespace acme::spark {

class SparkConfig;

class SparkBase {
    friend class SparkMax;

public:
    enum class IdleMode { kCoast = 0, kBrake = 1 };

    struct Faults {
        bool other;
        int rawBits;

        Faults() {}

        explicit Faults(uint16_t faults) {
            rawBits = faults;
            other = (faults & 0x1) != 0;
        }
    };

    typedef void (*Callback)(int);
    using Handle = int32_t;

    /**
     * Configure the controller.
     * @deprecated
     */
    virtual int Configure(SparkConfig &config);

    int GetValue() const noexcept;
    auto GetPair() -> std::pair<int, int>;

private:
    struct Hidden {
        int x;
    };
};

class SparkConfig {
public:
    enum IdleMode : uint32_t { kCoast = 0, kBrake = 1 };

    SparkConfig &SetIdleMode(IdleMode mode);
};

class SparkMax : public SparkBase {
public:
    SparkMax(int id) : SparkBase(), m_id(id), m_other{3} {}

private:
    int m_id;
    int m_other;
};

}  // namespace acme::spark

namespace wpi {
template <>
struct Struct<acme::spark::SparkBase::Faults> {
    static constexpr int GetSize() { return 4; }
};
}  // namespace wpi

#ifdef __GNUC__
#pragma GCC diagnostic pop
#endif
