#include "iso8583/codec.hpp"

#include <cassert>
#include <cstdint>
#include <stdexcept>
#include <vector>

int main() {
    const iso8583::spec message_spec({
        {2, {iso8583::length_type::llvar, 19}},
        {3, {iso8583::length_type::fixed, 6}},
        {4, {iso8583::length_type::fixed, 12}},
        {70, {iso8583::length_type::fixed, 3}},
    });

    iso8583::message original("0800");
    original.set_field(2, "4761739001010010");
    original.set_field(3, "000000");
    original.set_field(4, "000000001000");
    original.set_field(70, "301");

    std::vector<std::uint8_t> encoded;
    encoded.reserve(128);
    original.append_to(encoded, message_spec);
    const auto decoded = iso8583::parse(encoded, message_spec);

    assert(decoded.mti() == "0800");
    assert(decoded.field(2) == "4761739001010010");
    assert(decoded.field(3) == "000000");
    assert(decoded.field(4) == "000000001000");
    assert(decoded.field(70) == "301");

    encoded.pop_back();
    try {
        static_cast<void>(iso8583::parse(encoded, message_spec));
        assert(false && "truncated field was accepted");
    } catch (const std::invalid_argument&) {
    }
}