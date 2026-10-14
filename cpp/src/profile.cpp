#include "iso8583/profile.hpp"

#include <algorithm>
#include <cctype>
#include <iterator>
#include <limits>
#include <stdexcept>
#include <unordered_set>

namespace iso8583 {
namespace {

struct json_value {
    enum class kind { object, array, string, number, boolean, null } type{kind::null};
    std::unordered_map<std::string, json_value> object;
    std::vector<json_value> array;
    std::string string;
    std::size_t number{};
    bool boolean{};
};

class json_parser {
public:
    explicit json_parser(std::string_view input) : input_(input) {}

    json_value parse() {
        auto value = parse_value();
        whitespace();
        if (position_ != input_.size()) {
            fail("trailing data");
        }
        return value;
    }

private:
    json_value parse_value() {
        whitespace();
        if (position_ == input_.size()) fail("unexpected end");
        const auto character = input_[position_];
        if (character == '{') return parse_object();
        if (character == '[') return parse_array();
        if (character == '"') return make_string(parse_string());
        if (character >= '0' && character <= '9') return parse_number();
        if (consume("true")) return make_boolean(true);
        if (consume("false")) return make_boolean(false);
        if (consume("null")) return {};
        fail("invalid value");
    }

    json_value parse_object() {
        json_value value;
        value.type = json_value::kind::object;
        ++position_;
        whitespace();
        if (take('}')) return value;
        while (true) {
            whitespace();
            if (position_ == input_.size() || input_[position_] != '"') fail("object key must be a string");
            auto key = parse_string();
            whitespace();
            if (!take(':')) fail("expected colon");
            if (!value.object.emplace(std::move(key), parse_value()).second) fail("duplicate object key");
            whitespace();
            if (take('}')) return value;
            if (!take(',')) fail("expected comma");
        }
    }

    json_value parse_array() {
        json_value value;
        value.type = json_value::kind::array;
        ++position_;
        whitespace();
        if (take(']')) return value;
        while (true) {
            value.array.push_back(parse_value());
            whitespace();
            if (take(']')) return value;
            if (!take(',')) fail("expected comma");
        }
    }

    std::string parse_string() {
        ++position_;
        std::string output;
        while (position_ < input_.size()) {
            const auto character = input_[position_++];
            if (character == '"') return output;
            if (static_cast<unsigned char>(character) < 0x20) fail("control character in string");
            if (character != '\\') {
                output.push_back(character);
                continue;
            }
            if (position_ == input_.size()) fail("truncated escape");
            switch (input_[position_++]) {
                case '"': output.push_back('"'); break;
                case '\\': output.push_back('\\'); break;
                case '/': output.push_back('/'); break;
                case 'b': output.push_back('\b'); break;
                case 'f': output.push_back('\f'); break;
                case 'n': output.push_back('\n'); break;
                case 'r': output.push_back('\r'); break;
                case 't': output.push_back('\t'); break;
                default: fail("unsupported string escape");
            }
        }
        fail("unterminated string");
    }

    json_value parse_number() {
        const auto start = position_;
        while (position_ < input_.size() && std::isdigit(static_cast<unsigned char>(input_[position_]))) ++position_;
        json_value value;
        value.type = json_value::kind::number;
        try {
            value.number = static_cast<std::size_t>(std::stoull(std::string(input_.substr(start, position_ - start))));
        } catch (...) {
            fail("invalid number");
        }
        return value;
    }

    static json_value make_string(std::string value) {
        json_value output;
        output.type = json_value::kind::string;
        output.string = std::move(value);
        return output;
    }

    static json_value make_boolean(bool value) {
        json_value output;
        output.type = json_value::kind::boolean;
        output.boolean = value;
        return output;
    }

    void whitespace() {
        while (position_ < input_.size() && std::isspace(static_cast<unsigned char>(input_[position_]))) ++position_;
    }

    bool take(char expected) {
        if (position_ < input_.size() && input_[position_] == expected) {
            ++position_;
            return true;
        }
        return false;
    }

    bool consume(std::string_view token) {
        if (input_.substr(position_, token.size()) == token) {
            position_ += token.size();
            return true;
        }
        return false;
    }

    [[noreturn]] void fail(const char* message) const {
        throw std::invalid_argument("iso8583: invalid profile JSON at byte " + std::to_string(position_) + ": " + message);
    }

    std::string_view input_;
    std::size_t position_{};
};

const json_value& member(const json_value& value, const char* name, json_value::kind type) {
    if (value.type != json_value::kind::object) throw std::invalid_argument("iso8583: expected JSON object");
    const auto found = value.object.find(name);
    if (found == value.object.end() || found->second.type != type) {
        throw std::invalid_argument(std::string("iso8583: missing or invalid profile property ") + name);
    }
    return found->second;
}

void known_members(const json_value& value, std::initializer_list<const char*> names) {
    for (const auto& entry : value.object) {
        if (std::none_of(names.begin(), names.end(), [&](const char* name) { return entry.first == name; })) {
            throw std::invalid_argument("iso8583: unknown profile property " + entry.first);
        }
    }
}

std::size_t parse_field_number(const std::string& value) {
    if (value.empty() || !std::all_of(value.begin(), value.end(), [](unsigned char c) { return std::isdigit(c); })) {
        throw std::invalid_argument("iso8583: invalid field number " + value);
    }
    const auto number = static_cast<std::size_t>(std::stoul(value));
    if (number < 2 || number > max_field) throw std::invalid_argument("iso8583: field number is out of range");
    return number;
}

length_type parse_length_type(const std::string& value) {
    if (value == "fixed") return length_type::fixed;
    if (value == "llvar") return length_type::llvar;
    if (value == "lllvar") return length_type::lllvar;
    throw std::invalid_argument("iso8583: invalid length type " + value);
}

data_encoding parse_data_encoding(const std::string& value) {
    if (value == "ascii") return data_encoding::ascii;
    if (value == "binary") return data_encoding::binary;
    throw std::invalid_argument("iso8583: invalid field encoding " + value);
}

content_type parse_content(const std::string& value) {
    if (value == "numeric") return content_type::numeric;
    if (value == "text") return content_type::text;
    if (value == "bytes") return content_type::bytes;
    throw std::invalid_argument("iso8583: invalid field content " + value);
}

}  // namespace

transport_spec::transport_spec(std::size_t header_bytes, length_encoding encoding, std::size_t max_message_bytes)
    : header_bytes_(header_bytes), encoding_(encoding), max_message_bytes_(max_message_bytes) {
    if ((header_bytes != 0 && header_bytes != 2 && header_bytes != 4) || max_message_bytes == 0 ||
        (header_bytes == 0) != (encoding == length_encoding::none)) {
        throw std::invalid_argument("iso8583: invalid transport specification");
    }
}

void transport_spec::append_frame(std::vector<std::uint8_t>& destination, const std::uint8_t* payload, std::size_t size) const {
    if (size > max_message_bytes_) throw std::invalid_argument("iso8583: payload exceeds transport maximum");
    const auto start = destination.size();
    try {
        destination.resize(start + header_bytes_, 0);
        if (encoding_ == length_encoding::binary_big_endian) {
            if (header_bytes_ == 2 && size > 0xffff) throw std::invalid_argument("iso8583: payload does not fit transport header");
            for (std::size_t index = 0; index < header_bytes_; ++index) {
                destination[start + header_bytes_ - index - 1] = static_cast<std::uint8_t>(size >> (index * 8));
            }
        } else if (encoding_ == length_encoding::ascii) {
            auto remaining = size;
            for (auto index = start + header_bytes_; index-- > start;) {
                destination[index] = static_cast<std::uint8_t>('0' + remaining % 10);
                remaining /= 10;
            }
            if (remaining != 0) throw std::invalid_argument("iso8583: payload does not fit ASCII transport header");
        }
        destination.insert(destination.end(), payload, payload + size);
    } catch (...) {
        destination.resize(start);
        throw;
    }
}

std::string_view transport_spec::unframe(const std::uint8_t* frame, std::size_t size) const {
    if (header_bytes_ == 0) {
        if (size > max_message_bytes_) throw std::invalid_argument("iso8583: payload exceeds transport maximum");
        return {reinterpret_cast<const char*>(frame), size};
    }
    if (size < header_bytes_) throw std::invalid_argument("iso8583: transport header is truncated");
    std::size_t length = 0;
    for (std::size_t index = 0; index < header_bytes_; ++index) {
        if (encoding_ == length_encoding::binary_big_endian) {
            length = (length << 8) | frame[index];
        } else if (encoding_ == length_encoding::ascii) {
            if (frame[index] < '0' || frame[index] > '9') throw std::invalid_argument("iso8583: transport length is not decimal");
            length = length * 10 + frame[index] - '0';
        }
    }
    if (length > max_message_bytes_ || size - header_bytes_ != length) {
        throw std::invalid_argument("iso8583: transport frame length mismatch");
    }
    return {reinterpret_cast<const char*>(frame + header_bytes_), length};
}

profile::profile(std::string name, spec message_spec, std::array<field_definition, max_field + 1> fields,
                 std::unordered_map<std::string, message_rule> messages, transport_spec transport)
    : name_(std::move(name)), spec_(std::move(message_spec)), fields_(std::move(fields)),
      messages_(std::move(messages)), transport_(transport) {}

profile profile::load_json(std::istream& input) {
    const std::string json{std::istreambuf_iterator<char>(input), std::istreambuf_iterator<char>()};
    return parse_json(json);
}

profile profile::parse_json(std::string_view json) {
    const auto root = json_parser(json).parse();
    known_members(root, {"name", "transport", "fields", "messages"});
    const auto name = member(root, "name", json_value::kind::string).string;
    if (name.empty()) throw std::invalid_argument("iso8583: profile name is required");

    const auto& transport_json = member(root, "transport", json_value::kind::object);
    known_members(transport_json, {"headerBytes", "lengthEncoding", "maxMessageBytes"});
    const auto header_bytes = member(transport_json, "headerBytes", json_value::kind::number).number;
    const auto maximum = member(transport_json, "maxMessageBytes", json_value::kind::number).number;
    length_encoding header_encoding = length_encoding::none;
    if (header_bytes != 0) {
        const auto& encoding = member(transport_json, "lengthEncoding", json_value::kind::string).string;
        if (encoding == "binary-big-endian") header_encoding = length_encoding::binary_big_endian;
        else if (encoding == "ascii") header_encoding = length_encoding::ascii;
        else throw std::invalid_argument("iso8583: invalid transport length encoding");
    }
    transport_spec transport(header_bytes, header_encoding, maximum);

    std::array<field_definition, max_field + 1> fields{};
    std::unordered_map<std::size_t, field_spec> specifications;
    for (const auto& entry : member(root, "fields", json_value::kind::object).object) {
        const auto number = parse_field_number(entry.first);
        const auto& value = entry.second;
        known_members(value, {"name", "lengthType", "length", "encoding", "content", "sensitive"});
        const auto field = field_spec{parse_length_type(member(value, "lengthType", json_value::kind::string).string),
                                      member(value, "length", json_value::kind::number).number};
        const auto encoding = parse_data_encoding(member(value, "encoding", json_value::kind::string).string);
        const auto content = parse_content(member(value, "content", json_value::kind::string).string);
        if (encoding == data_encoding::binary && content != content_type::bytes) {
            throw std::invalid_argument("iso8583: binary field must use bytes content");
        }
        auto sensitive = false;
        const auto found_sensitive = value.object.find("sensitive");
        if (found_sensitive != value.object.end()) {
            if (found_sensitive->second.type != json_value::kind::boolean) throw std::invalid_argument("iso8583: invalid sensitive flag");
            sensitive = found_sensitive->second.boolean;
        }
        fields[number] = {field, member(value, "name", json_value::kind::string).string, encoding, content, sensitive};
        if (fields[number].name.empty()) throw std::invalid_argument("iso8583: field name is required");
        specifications.emplace(number, field);
    }
    spec message_spec(specifications);

    std::unordered_map<std::string, message_rule> messages;
    for (const auto& entry : member(root, "messages", json_value::kind::object).object) {
        if (entry.first.size() != 4) throw std::invalid_argument("iso8583: invalid MTI rule");
        known_members(entry.second, {"required"});
        std::unordered_set<std::size_t> seen;
        message_rule rule;
        for (const auto& required : member(entry.second, "required", json_value::kind::array).array) {
            if (required.type != json_value::kind::number || required.number < 2 || required.number > max_field ||
                fields[required.number].field.length == 0 || !seen.insert(required.number).second) {
                throw std::invalid_argument("iso8583: invalid required field");
            }
            rule.required.push_back(required.number);
        }
        messages.emplace(entry.first, std::move(rule));
    }
    if (messages.empty()) throw std::invalid_argument("iso8583: profile must define at least one MTI");
    return profile(name, std::move(message_spec), std::move(fields), std::move(messages), transport);
}

const field_definition& profile::field(std::size_t number) const {
    if (number < 2 || number > max_field || fields_[number].field.length == 0) {
        throw std::out_of_range("iso8583: field is not defined by profile");
    }
    return fields_[number];
}

void profile::validate(const message& value) const {
    const auto rule = messages_.find(std::string(value.mti()));
    if (rule == messages_.end()) throw std::invalid_argument("iso8583: MTI is not allowed by profile");
    for (const auto number : rule->second.required) {
        if (value.field(number).empty()) throw std::invalid_argument("iso8583: required field is missing");
    }
    for (std::size_t number = 2; number <= max_field; ++number) {
        const auto field_value = value.field(number);
        if (field_value.empty()) continue;
        const auto& definition = field(number);
        if ((definition.field.type == length_type::fixed && field_value.size() != definition.field.length) ||
            (definition.field.type != length_type::fixed && field_value.size() > definition.field.length)) {
            throw std::invalid_argument("iso8583: field has invalid length");
        }
        for (const auto character : field_value) {
            const auto byte = static_cast<unsigned char>(character);
            if (definition.encoding == data_encoding::ascii && byte > 0x7f) throw std::invalid_argument("iso8583: field is not ASCII");
            if (definition.content == content_type::numeric && (character < '0' || character > '9')) throw std::invalid_argument("iso8583: field is not numeric");
            if (definition.content == content_type::text && (byte < 0x20 || byte > 0x7e)) throw std::invalid_argument("iso8583: field is not printable text");
        }
    }
}

}  // namespace iso8583