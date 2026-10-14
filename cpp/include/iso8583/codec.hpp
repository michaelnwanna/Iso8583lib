#pragma once

#include <array>
#include <cstddef>
#include <cstdint>
#include <string>
#include <string_view>
#include <unordered_map>
#include <vector>

namespace iso8583 {

constexpr std::size_t max_field = 128;

enum class length_type : std::uint8_t { fixed, llvar, lllvar };
enum class data_encoding : std::uint8_t { ascii, binary, packed_bcd };
enum class field_length_encoding : std::uint8_t { ascii, bcd };

struct field_spec {
    length_type type;
    std::size_t length;
    data_encoding encoding{data_encoding::ascii};
    field_length_encoding length_encoding{field_length_encoding::ascii};
};

class spec {
public:
    explicit spec(const std::unordered_map<std::size_t, field_spec>& fields);
    const field_spec& operator[](std::size_t number) const noexcept;

private:
    std::array<field_spec, max_field + 1> fields_{};
};

class message {
public:
    explicit message(std::string_view mti);

    std::string_view mti() const noexcept;
    std::string_view field(std::size_t number) const;
    void set_field(std::size_t number, std::string value);
    void append_to(std::vector<std::uint8_t>& destination, const spec& message_spec) const;

private:
    friend message parse(const std::uint8_t*, std::size_t, const spec&);
    std::array<char, 4> mti_{};
    std::array<std::string, max_field + 1> fields_{};
};

message parse(const std::uint8_t* data, std::size_t size, const spec& message_spec);
inline message parse(const std::vector<std::uint8_t>& data, const spec& message_spec) {
    return parse(data.data(), data.size(), message_spec);
}

}  // namespace iso8583