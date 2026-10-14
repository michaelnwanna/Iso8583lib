#include "iso8583/codec.hpp"

#include <cstdint>
#include <vector>

int main() {
    const iso8583::spec spec({{3, {iso8583::length_type::fixed, 6}}});
    iso8583::message message("0800");
    message.set_field(3, "000000");
    std::vector<std::uint8_t> encoded;
    message.append_to(encoded, spec);
    return iso8583::parse(encoded, spec).field(3) == "000000" ? 0 : 1;
}