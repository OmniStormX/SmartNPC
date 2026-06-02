// Wire protocol DTOs for the ws bridge. See docs/protocol.md for the spec.

using System.Collections.Generic;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace SmartNPC.Bridge
{
    /// <summary>Inbound request from the MCP client.</summary>
    /// <remarks>
    /// <see cref="FromNpc"/> carries the originating Hermes-profile NPC name
    /// (stamped by smartnpc-mcp's WSClient.Call when the calling MCP session
    /// has registered itself via the <c>agent_register_self</c> tool, or by
    /// CallAs for operator-side fan-out). Used by the debug-mirror feature
    /// to route every inbound tool call to the correct NPC's chat-panel
    /// conversation, even for NPC-agnostic queries (game_get_time,
    /// game_get_weather, mail_send, …) whose own params carry no NPC field.
    /// May be empty for legacy / operator callers.
    /// </remarks>
    internal sealed class Request
    {
        [JsonPropertyName("type")]     public string? Type    { get; set; }  // "request"
        [JsonPropertyName("id")]       public string? Id      { get; set; }
        [JsonPropertyName("action")]   public string? Action  { get; set; }
        [JsonPropertyName("params")]   public JsonElement Params { get; set; }
        [JsonPropertyName("from_npc")] public string? FromNpc { get; set; }
    }

    /// <summary>Outbound response to a request.</summary>
    internal sealed class Response
    {
        [JsonPropertyName("type")]  public string Type { get; set; } = "response";
        [JsonPropertyName("id")]    public string Id   { get; set; } = "";
        [JsonPropertyName("ok")]    public bool   Ok   { get; set; }
        [JsonPropertyName("data")]  [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)] public object? Data  { get; set; }
        [JsonPropertyName("error")] [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)] public ResponseError? Error { get; set; }

        public static Response Success(string id, object? data) =>
            new() { Id = id, Ok = true, Data = data };

        public static Response Failure(string id, string code, string message) =>
            new() { Id = id, Ok = false, Error = new ResponseError { Code = code, Message = message } };
    }

    internal sealed class ResponseError
    {
        [JsonPropertyName("code")]    public string Code    { get; set; } = "";
        [JsonPropertyName("message")] public string Message { get; set; } = "";
    }

    /// <summary>Server-initiated event push.</summary>
    internal sealed class Event
    {
        [JsonPropertyName("type")]      public string Type      { get; set; } = "event";
        [JsonPropertyName("name")]      public string Name      { get; set; } = "";
        [JsonPropertyName("data")]      public object? Data     { get; set; }
        [JsonPropertyName("timestamp")] public long   Timestamp { get; set; }
    }

    internal static class JsonOpts
    {
        public static readonly JsonSerializerOptions Web = new(JsonSerializerDefaults.Web)
        {
            DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull,
        };
    }
}
