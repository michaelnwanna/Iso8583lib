#include "iso8583/codec.hpp"
#include "iso8583/profile.hpp"

#include <cstdint>
#include <fstream>
#include <stdexcept>
#include <string>
#include <unordered_map>
#include <vector>

namespace {

void require(bool condition, const char* message) {
    if (!condition) {
        throw std::runtime_error(message);
    }
}

struct conformance_vector {
    std::string name;
    std::string mti;
    std::unordered_map<std::size_t, std::string> fields;
    std::vector<std::uint8_t> payload;
    std::vector<std::uint8_t> frame;
};

std::vector<std::string> split(const std::string& value, char delimiter) {
    std::vector<std::string> parts;
    std::size_t start = 0;
    while (true) {
        const auto end = value.find(delimiter, start);
        parts.push_back(value.substr(start, end - start));
        if (end == std::string::npos) return parts;
        start = end + 1;
    }
}

std::vector<std::uint8_t> decode_hex(const std::string& value) {
    if (value.size() % 2 != 0) throw std::runtime_error("odd conformance hex length");
    std::vector<std::uint8_t> output;
    output.reserve(value.size() / 2);
    auto digit = [](char character) -> std::uint8_t {
        if (character >= '0' && character <= '9') return static_cast<std::uint8_t>(character - '0');
        if (character >= 'A' && character <= 'F') return static_cast<std::uint8_t>(character - 'A' + 10);
        if (character >= 'a' && character <= 'f') return static_cast<std::uint8_t>(character - 'a' + 10);
        throw std::runtime_error("invalid conformance hex digit");
    };
    for (std::size_t index = 0; index < value.size(); index += 2) {
        output.push_back(static_cast<std::uint8_t>((digit(value[index]) << 4) | digit(value[index + 1])));
    }
    return output;
}

std::vector<conformance_vector> load_vectors(const char* path) {
    std::ifstream input(path);
    if (!input) throw std::runtime_error("conformance vectors could not be opened");
    std::vector<conformance_vector> vectors;
    std::string line;
    while (std::getline(input, line)) {
        if (line.empty() || line.front() == '#') continue;
        const auto parts = split(line, '|');
        if (parts.size() != 5) throw std::runtime_error("invalid conformance vector");
        conformance_vector vector{parts[0], parts[1], {}, decode_hex(parts[3]), decode_hex(parts[4])};
        for (const auto& pair : split(parts[2], ';')) {
            const auto separator = pair.find('=');
            if (separator == std::string::npos) throw std::runtime_error("invalid conformance field");
            vector.fields.emplace(static_cast<std::size_t>(std::stoul(pair.substr(0, separator))), pair.substr(separator + 1));
        }
        vectors.push_back(std::move(vector));
    }
    if (vectors.empty()) throw std::runtime_error("no conformance vectors loaded");
    return vectors;
}

}  // namespace

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

    require(decoded.mti() == "0800", "MTI did not round trip");
    require(decoded.field(2) == "4761739001010010", "field 2 did not round trip");
    require(decoded.field(3) == "000000", "field 3 did not round trip");
    require(decoded.field(4) == "000000001000", "field 4 did not round trip");
    require(decoded.field(70) == "301", "field 70 did not round trip");

    encoded.pop_back();
    try {
        static_cast<void>(iso8583::parse(encoded, message_spec));
        throw std::runtime_error("truncated field was accepted");
    } catch (const std::invalid_argument&) {
    }

    const iso8583::spec financial_spec({
        {2, {iso8583::length_type::llvar, 19}}, {3, {iso8583::length_type::fixed, 6}},
        {4, {iso8583::length_type::fixed, 12}}, {7, {iso8583::length_type::fixed, 10}},
        {11, {iso8583::length_type::fixed, 6}}, {12, {iso8583::length_type::fixed, 6}},
        {13, {iso8583::length_type::fixed, 4}}, {14, {iso8583::length_type::fixed, 4}},
        {18, {iso8583::length_type::fixed, 4}}, {22, {iso8583::length_type::fixed, 3}},
        {25, {iso8583::length_type::fixed, 2}}, {32, {iso8583::length_type::llvar, 11}},
        {37, {iso8583::length_type::fixed, 12}}, {38, {iso8583::length_type::fixed, 6}},
        {39, {iso8583::length_type::fixed, 2}}, {41, {iso8583::length_type::fixed, 8}},
        {42, {iso8583::length_type::fixed, 15}}, {49, {iso8583::length_type::fixed, 3}},
    });
    iso8583::message request("0100");
    request.set_field(2, "4111111111111111");
    request.set_field(3, "000000");
    request.set_field(4, "000000012500");
    request.set_field(7, "1014123456");
    request.set_field(11, "123456");
    request.set_field(12, "123456");
    request.set_field(13, "1014");
    request.set_field(14, "2912");
    request.set_field(18, "5411");
    request.set_field(22, "051");
    request.set_field(25, "00");
    request.set_field(32, "12345678901");
    request.set_field(37, "628812345678");
    request.set_field(41, "TERM0001");
    request.set_field(42, "MERCHANT0000001");
    request.set_field(49, "840");

    std::vector<std::uint8_t> wire_request;
    wire_request.reserve(256);
    request.append_to(wire_request, financial_spec);
    const auto acquirer_request = iso8583::parse(wire_request, financial_spec);
    require(acquirer_request.mti() == "0100", "acquirer received the wrong MTI");
    require(acquirer_request.field(4) == "000000012500", "amount did not round trip");
    require(acquirer_request.field(41) == "TERM0001", "terminal ID did not round trip");

    iso8583::message response("0110");
    response.set_field(3, "000000");
    response.set_field(4, "000000012500");
    response.set_field(11, "123456");
    response.set_field(37, "628812345678");
    response.set_field(38, "A12345");
    response.set_field(39, "00");
    response.set_field(41, "TERM0001");
    response.set_field(42, "MERCHANT0000001");
    response.set_field(49, "840");

    std::vector<std::uint8_t> wire_response;
    wire_response.reserve(128);
    response.append_to(wire_response, financial_spec);
    const auto terminal_response = iso8583::parse(wire_response, financial_spec);
    require(terminal_response.mti() == "0110", "terminal received the wrong MTI");
    require(terminal_response.field(39) == "00", "authorization was not approved");
    require(terminal_response.field(38) == "A12345", "authorization ID did not round trip");
    require(terminal_response.field(37) == acquirer_request.field(37), "RRN did not correlate");

    std::ifstream profile_file(ISO8583_EXAMPLE_PROFILE);
    require(profile_file.good(), "example profile could not be opened");
    const auto configured_profile = iso8583::profile::load_json(profile_file);
    configured_profile.validate(request);
    std::vector<std::uint8_t> configured_payload;
    request.append_to(configured_payload, configured_profile.message_spec());
    std::vector<std::uint8_t> configured_frame;
    configured_profile.transport().append_frame(configured_frame, configured_payload);
    const auto unframed = configured_profile.transport().unframe(configured_frame);
    const auto configured_message = iso8583::parse(
        reinterpret_cast<const std::uint8_t*>(unframed.data()), unframed.size(), configured_profile.message_spec());
    configured_profile.validate(configured_message);
    require(configured_profile.field(2).sensitive, "PAN metadata is not marked sensitive");

    for (const auto& vector : load_vectors(ISO8583_CONFORMANCE_VECTORS)) {
        iso8583::message vector_message(vector.mti);
        for (const auto& entry : vector.fields) vector_message.set_field(entry.first, entry.second);
        configured_profile.validate(vector_message);

        std::vector<std::uint8_t> vector_payload;
        vector_message.append_to(vector_payload, configured_profile.message_spec());
        require(vector_payload == vector.payload, "payload differs from shared conformance vector");
        std::vector<std::uint8_t> vector_frame;
        configured_profile.transport().append_frame(vector_frame, vector_payload);
        require(vector_frame == vector.frame, "frame differs from shared conformance vector");

        const auto vector_decoded = iso8583::parse(vector.payload, configured_profile.message_spec());
        configured_profile.validate(vector_decoded);
        for (const auto& entry : vector.fields) {
            require(vector_decoded.field(entry.first) == entry.second, "decoded field differs from shared conformance vector");
        }
    }

    iso8583::message incomplete_request("0100");
    incomplete_request.set_field(2, "4111111111111111");
    try {
        configured_profile.validate(incomplete_request);
        throw std::runtime_error("profile accepted a request missing required fields");
    } catch (const std::invalid_argument&) {
    }
}