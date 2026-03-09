from __future__ import annotations

import asyncio
import base64
import json
import logging
import time
from collections.abc import AsyncGenerator
from dataclasses import dataclass

import anthropic
from google import genai
from openai import AsyncOpenAI

from config import settings
from services import tool_executor

logger = logging.getLogger(__name__)


@dataclass
class TokenUsage:
    prompt_tokens: int
    completion_tokens: int


@dataclass
class ToolCallEvent:
    content: str  # e.g. "Web Searching: latest AI news"


_openai_clients: dict[str, AsyncOpenAI] = {}
_claude_client: anthropic.AsyncAnthropic | None = None
_gemini_client: genai.Client | None = None

# Provider credential lookup for OpenAI-compatible providers
_OPENAI_PROVIDER_CREDENTIALS = {
    "deepseek": lambda: (settings.deepseek_api_key, settings.deepseek_base_url),
    "openai": lambda: (settings.openai_api_key, settings.openai_base_url),
}


def _get_openai_client(provider: str) -> AsyncOpenAI:
    """Get or create an AsyncOpenAI client for OpenAI-compatible providers."""
    if provider not in _openai_clients:
        cred_fn = _OPENAI_PROVIDER_CREDENTIALS[provider]
        api_key, base_url = cred_fn()
        _openai_clients[provider] = AsyncOpenAI(api_key=api_key, base_url=base_url)
    return _openai_clients[provider]


def _get_claude_client() -> anthropic.AsyncAnthropic:
    """Get or create an AsyncAnthropic client."""
    global _claude_client
    if _claude_client is None:
        _claude_client = anthropic.AsyncAnthropic(api_key=settings.claude_api_key)
    return _claude_client


def _get_gemini_client() -> genai.Client:
    """Get or create a Gemini client."""
    global _gemini_client
    if _gemini_client is None:
        _gemini_client = genai.Client(api_key=settings.gemini_api_key)
    return _gemini_client


def _resolve_model(model_index: int) -> tuple[str, str]:
    """Map the integer model index to (provider, model_name)."""
    entry = settings.model_map.get(model_index, settings.model_map[0])
    return entry["provider"], entry["model"]


# ── Shared behavioural instructions (appended to every provider prompt) ──

_SHARED_INSTRUCTIONS = """

## Formatting
- When writing mathematical expressions, always use LaTeX syntax: \
use $...$ for inline math and $$...$$ for block/display math. \
For example: $E = mc^2$ for inline, or $$\\sum_{i=1}^{n} x_i$$ for display.
- Use Markdown where it improves readability (code fences, tables, lists). \
Avoid excessive formatting for short or conversational replies.
- **NEVER display raw URLs** in your response. Always format URLs as \
Markdown links: `[descriptive title](url)`. For example, write \
`[NBA Standings - ESPN](https://espn.com/nba/standings)` instead of \
`https://espn.com/nba/standings`.

## Provided Context
- The conversation may contain **files, images, or documents** attached by \
the user (often in earlier messages). **ALWAYS examine and use this provided \
context first** before considering any tool calls. If the user's question \
can be answered from the attached content, answer directly — do NOT use \
web_search or url_reader to look up information that is already present \
in the conversation.

## Tool Usage
- You have access to tools such as web_search and url_reader. \
Use them **proactively** whenever a question involves recent events, \
real-time data, current status, rankings, scores, prices, news, or \
any topic that may have changed after your training cutoff.
- **Do NOT refuse to answer** by saying "I cannot predict the future" or \
"it's too early to tell." Instead, use web_search to find the latest \
information and provide a well-informed answer based on the results.
- When search results conflict with your internal knowledge, **prefer \
the search results** — they are more up-to-date.
- Always try your best to answer the user's question. Use tools to \
supplement your knowledge rather than declining to respond.
- When your answer is based on web search or url_reader results, \
place citation links **inline** right after the relevant sentence or claim, \
NOT collected at the end. Use this exact Markdown format for each citation: \
`[SiteName](url "source")` — note the `"source"` title is **required**. \
Keep the link text short (site name or brief label). For example: \
`Based on the latest data, Jokic is averaging a triple-double [ESPN](https://espn.com/... "source"), \
leading the MVP race [NBA.com](https://nba.com/... "source").` \
Do NOT group all sources at the bottom. Each claim should have its own \
inline citation immediately after it.
- **Multilingual search** — regardless of what language the user writes \
in, always consider searching in **both** the user's language AND English. \
Many authoritative primary sources (ESPN, Reuters, official league sites, \
government databases, tech documentation, academic papers) publish in \
English. If the topic is international (sports, finance, science, tech, \
world news), perform at least one English-language search in addition to \
any searches in the user's language to ensure you find the best sources.
- **Source priority** — when multiple sources conflict, prefer them \
in this order: (1) newer data over older data, (2) official/primary \
sources (government sites, league official sites, company blogs) over \
reposts/aggregators, (3) reposts/aggregators over informal/user-generated \
content. Always cite and base your answer on the highest-priority sources.

## Accuracy & Honesty
- Be direct and honest. Do not use ungrounded flattery or sycophancy. \
Never open a response by praising the user's question.
- If evidence is insufficient to answer even after searching, say so \
clearly. Never fabricate facts, citations, URLs, or data.
- If the user references an image, file, or attachment that is not present \
in the conversation, explicitly tell them you cannot see it and ask them \
to provide it.
- Express uncertainty when appropriate rather than guessing.

## Tone
- Keep your tone natural, warm, and concise.
- Give brief answers to simple questions; provide thorough answers to \
complex or open-ended questions.
- Respond in the same language the user writes in, unless asked otherwise.
"""

# Identity blocks per provider (prepended to the shared instructions)
_PROVIDER_IDENTITY = {
    "openai": (
        "You are GPT 5.2, the latest large language model by OpenAI. "
        "When asked about your identity or model version explicitly, "
        "you must always say you are GPT 5.2."
    ),
    "gemini": (
        "You are Gemini 3.1 Pro, the latest large language model by Google. "
        "When asked about your identity or model version explicitly, "
        "you must always say you are Gemini 3.1 Pro.\n\n"
        "CRITICAL RULE — Web Search Trust Policy:\n"
        "Your training data is a FROZEN SNAPSHOT from the past. The real world "
        "has moved on. When you use web_search and multiple results consistently "
        "state a fact (e.g. a player was traded, a company was acquired, a law "
        "was passed), that fact is TRUE — even if it contradicts what you "
        "learned during training.\n"
        "- NEVER say search results are 'unverified', 'rumored', 'possibly "
        "future', or 'data errors.'\n"
        "- NEVER prefer your training data over consistent search results.\n"
        "- If 2+ search results agree on something, treat it as confirmed fact "
        "and state it directly to the user.\n"
        "- When you need more information, you MUST actually call web_search — "
        "do NOT just say 'I need to search more' in text without calling the tool.\n"
    ),
    "deepseek": (
        "You are DeepSeek V3, the latest large language model by DeepSeek. "
        "When asked about your identity or model version explicitly, "
        "you must always say you are DeepSeek V3."
    ),
    "claude": (
        "You are Claude Opus 4.6, the latest large language model by Anthropic. "
        "When asked about your identity or model version explicitly, "
        "you must always say you are Claude Opus 4.6."
    ),
}


def _get_system_prompt(provider: str) -> str:
    """Build the full system prompt for a provider, with today's date injected."""
    from datetime import datetime, timezone
    today = datetime.now(timezone.utc).strftime("%Y-%m-%d")
    identity = _PROVIDER_IDENTITY.get(provider, "")
    return (
        f"{identity}\n\n"
        f"Current date: {today} (UTC). "
        f"The current year is {today[:4]}. When users ask about "
        f"\"this season\", \"now\", \"latest\", or \"current\", they mean "
        f"the {int(today[:4])-1}-{today[:4]} season (for sports) or the year "
        f"{today[:4]}. Always include the current year/season in your "
        f"web_search queries to avoid returning outdated results.\n"
        f"{_SHARED_INSTRUCTIONS}"
    )


def _extract_system_and_messages(
    messages: list[dict],
) -> tuple[str, list[dict]]:
    """Separate system messages from chat messages.

    Returns (system_prompt, chat_messages) where system_prompt is the
    concatenation of all system messages and chat_messages contains
    the remaining user/assistant messages.
    """
    system_parts: list[str] = []
    chat_messages: list[dict] = []
    for msg in messages:
        if msg["role"] == "system":
            system_parts.append(msg["content"])
        else:
            chat_messages.append({"role": msg["role"], "content": msg["content"]})
    return "\n".join(system_parts), chat_messages


def _parse_data_uri(data_uri: str) -> tuple[str, str]:
    """Parse a data URI into (media_type, base64_data)."""
    # "data:image/jpeg;base64,/9j/..." -> ("image/jpeg", "/9j/...")
    header, data = data_uri.split(",", 1)
    media_type = header.split(":")[1].split(";")[0]
    return media_type, data


def _convert_to_claude_content(content: str | list[dict]) -> str | list[dict]:
    """Convert content to Claude message format.

    Claude expects image blocks as:
      {"type": "image", "source": {"type": "base64", "media_type": "...", "data": "..."}}
    """
    if isinstance(content, str):
        return content
    claude_blocks: list[dict] = []
    for block in content:
        if block["type"] == "text":
            claude_blocks.append({"type": "text", "text": block["text"]})
        elif block["type"] == "image_url":
            media_type, data = _parse_data_uri(block["image_url"]["url"])
            claude_blocks.append({
                "type": "image",
                "source": {
                    "type": "base64",
                    "media_type": media_type,
                    "data": data,
                },
            })
    return claude_blocks


def _convert_to_gemini_parts(content: str | list[dict]) -> list[genai.types.Part]:
    """Convert content to a list of Gemini Part objects."""
    if isinstance(content, str):
        return [genai.types.Part(text=content)]
    parts: list[genai.types.Part] = []
    for block in content:
        if block["type"] == "text":
            parts.append(genai.types.Part(text=block["text"]))
        elif block["type"] == "image_url":
            media_type, data = _parse_data_uri(block["image_url"]["url"])
            parts.append(genai.types.Part(
                inline_data=genai.types.Blob(
                    mime_type=media_type,
                    data=base64.b64decode(data),
                ),
            ))
    return parts


def _convert_to_openai_content(content: str | list[dict]) -> str | list[dict]:
    """Convert content to OpenAI vision format.

    Explicitly builds content blocks with the ``detail`` parameter for images.
    """
    if isinstance(content, str):
        return content
    oai_blocks: list[dict] = []
    for block in content:
        if block["type"] == "text":
            oai_blocks.append({"type": "text", "text": block["text"]})
        elif block["type"] == "image_url":
            oai_blocks.append({
                "type": "image_url",
                "image_url": {
                    "url": block["image_url"]["url"],
                    "detail": "auto",
                },
            })
    return oai_blocks


def _strip_image_blocks(content: str | list[dict]) -> str:
    """Strip image blocks for providers that don't support multimodal.

    Returns a plain string with only the text portions.
    """
    if isinstance(content, str):
        return content
    text_parts = [block["text"] for block in content if block["type"] == "text"]
    return "\n".join(text_parts) if text_parts else ""


async def generate_title(messages: list[dict]) -> str:
    """Generate a short conversation title from the conversation context."""
    entry = settings.utility_model
    provider = entry["provider"]
    model = entry["model"]

    system_prompt = (
        "You are a title generator. Output a short title (4-10 words) summarizing the conversation topic. "
        "Match the user's language. Do NOT answer the user's question. Do NOT describe or analyze images. "
        "Output ONLY the title, nothing else."
    )

    parts = []
    for msg in messages:
        content = msg["content"] if isinstance(msg["content"], str) else "[multimodal content]"
        parts.append(f"[{msg['role']}]: {content}")
    combined = "\n\n".join(parts)

    if provider == "claude":
        client = _get_claude_client()
        response = await client.messages.create(
            model=model,
            max_tokens=50,
            system=system_prompt,
            messages=[{"role": "user", "content": combined}],
        )
        return response.content[0].text.strip() if response.content else "New Chat"

    if provider == "gemini":
        client = _get_gemini_client()
        response = await client.aio.models.generate_content(
            model=model,
            contents=[genai.types.Content(
                role="user",
                parts=[genai.types.Part(text=f"{combined}\n\n---\n{system_prompt}")],
            )],
            config=genai.types.GenerateContentConfig(
                max_output_tokens=100,
                temperature=0.7,
            ),
        )
        return response.text.strip() if response.text else "New Chat"

    # OpenAI-compatible providers
    client = _get_openai_client(provider)
    oai_messages: list[dict] = [
        {"role": "system", "content": system_prompt},
        {"role": "user", "content": combined},
    ]
    response = await client.chat.completions.create(
        model=model,
        messages=oai_messages,
        max_tokens=50,
        temperature=0.7,
    )
    return response.choices[0].message.content.strip()


_MESSAGE_LEVEL_SUMMARY_PROMPT = (
    "You are a conversation history compression assistant. Your task is to compress "
    "conversation history into a concise summary that an AI assistant can use as context "
    "to seamlessly continue the conversation.\n\n"
    "## Information to Preserve (by priority)\n\n"
    "1. User's core needs: what problem, feature, or goal the user is working toward\n"
    "2. Decisions and conclusions: agreed-upon solutions, technical choices, design decisions\n"
    "3. Key technical details: file paths, function names, config values, API endpoints, "
    "data structures, and other concrete references\n"
    "4. Unresolved issues: items still under discussion or pending — prefix with \"Pending:\"\n"
    "5. Important context: background info, constraints, preferences, and environment details "
    "provided by the user\n\n"
    "## Format Rules\n\n"
    "- Write in concise natural-language paragraphs; use bullet points (\u00b7) only for parallel items\n"
    "- Do NOT use headings, numbered lists, blockquotes, or other Markdown formatting\n"
    "- Keep code snippets only when essential for understanding context; trim to key lines\n"
    "- For long assistant replies, keep only conclusions and key points \u2014 omit reasoning steps\n"
    "- Target length: 1/5 to 1/3 of the original content; maximize information density\n"
    "- Output the summary text only \u2014 no prefixes, suffixes, or meta-commentary\n"
    "- Write the summary in the same language as the conversation"
)

_NODE_LEVEL_SUMMARY_PROMPT = (
    "You are a conversation summarization assistant. Your task is to produce a brief context "
    "overview of a conversation so that a different, related conversation can use it as "
    "background knowledge. The AI reading this overview needs to quickly understand what was "
    "discussed and what conclusions were reached \u2014 not the back-and-forth process.\n\n"
    "## Information to Preserve (by priority)\n\n"
    "1. Topic: what core subject this conversation was about\n"
    "2. Final conclusions and outputs: what solution was chosen, what was produced, what decisions were made\n"
    "3. Key facts: important technical details, data points, constraints, and objective information\n"
    "4. Open items: any unresolved issues or follow-up plans, briefly noted\n\n"
    "## Format Rules\n\n"
    "- Write in concise natural-language paragraphs; use bullet points (\u00b7) only for parallel items\n"
    "- Do NOT use headings, numbered lists, blockquotes, or other Markdown formatting\n"
    "- Focus on outcomes and conclusions \u2014 omit exploratory back-and-forth details\n"
    "- Keep code snippets only when essential for understanding conclusions\n"
    "- Keep the summary concise \u2014 typically no more than 500 words\n"
    "- Output the summary text only \u2014 no prefixes, suffixes, or meta-commentary\n"
    "- Write the summary in the same language as the conversation"
)


def _build_summary_user_content(
    messages: list[dict], previous_summary: str | None
) -> str:
    """Build the user content for summary generation from messages and optional previous summary."""
    parts: list[str] = []
    if previous_summary:
        parts.append(f"[Previous summary]\n{previous_summary}\n")
    parts.append("[New conversation to summarize]")
    for msg in messages:
        role_label = "User" if msg["role"] == "user" else "Assistant"
        content = msg["content"]
        if isinstance(content, list):
            # Multimodal: extract text blocks only for summarization
            text_parts = [
                block["text"] for block in content if block.get("type") == "text" and block.get("text")
            ]
            content = "\n".join(text_parts) if text_parts else "(non-text content)"
        parts.append(f"{role_label}: {content}")
    return "\n".join(parts)


async def generate_summary(
    messages: list[dict],
    previous_summary: str | None = None,
    summary_type: str = "message",
) -> str:
    """Generate a conversation summary using a large-context, cheap model.

    Args:
        messages: Raw conversation messages to summarize.
        previous_summary: Previous summary to extend (if any).
        summary_type: "message" for message-level (incremental compression within a node)
                      or "node" for node-level (cross-node context briefing).

    Returns:
        The generated summary text.
    """
    entry = settings.summary_model
    provider = entry["provider"]
    model = entry["model"]

    system_prompt = (
        _NODE_LEVEL_SUMMARY_PROMPT
        if summary_type == "node"
        else _MESSAGE_LEVEL_SUMMARY_PROMPT
    )

    user_content = _build_summary_user_content(messages, previous_summary)

    if provider == "claude":
        client = _get_claude_client()
        response = await client.messages.create(
            model=model,
            max_tokens=10240,
            system=system_prompt,
            messages=[{"role": "user", "content": user_content}],
        )
        return response.content[0].text.strip()

    if provider == "gemini":
        client = _get_gemini_client()
        response = await client.aio.models.generate_content(
            model=model,
            contents=user_content,
            config=genai.types.GenerateContentConfig(
                system_instruction=system_prompt,
                max_output_tokens=10240,
                temperature=0.3,
            ),
        )
        return response.text.strip() if response.text else ""

    # OpenAI-compatible providers
    client = _get_openai_client(provider)
    response = await client.chat.completions.create(
        model=model,
        messages=[
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": user_content},
        ],
        max_tokens=10240,
        temperature=0.3,
    )
    return response.choices[0].message.content.strip()


# ── Tool Call Loop ──────────────────────────────────────────────

_MAX_TOOL_ROUNDS = 10
_MAX_TOOL_CALLS_PER_ROUND = 3
_TOOL_LOOP_TIMEOUT = 120.0

# Provider-agnostic tool definitions
_TOOL_DEFINITIONS = [
    {
        "name": "web_search",
        "description": (
            "Search the web for current information, recent events, or data "
            "that may not be in your training data. Use this when the user asks "
            "about current events, recent developments, or any topic requiring "
            "up-to-date information."
        ),
        "parameters": {
            "type": "object",
            "properties": {
                "query": {
                    "type": "string",
                    "description": "The search query, in the language most likely to yield good results",
                }
            },
            "required": ["query"],
        },
    },
    {
        "name": "url_reader",
        "description": (
            "Read and extract the main content from a specific URL. Use this "
            "when you need to access the full content of a web page, such as "
            "reading an article, documentation, or any web resource."
        ),
        "parameters": {
            "type": "object",
            "properties": {
                "url": {
                    "type": "string",
                    "description": "The complete URL to read (must start with http:// or https://)",
                }
            },
            "required": ["url"],
        },
    },
]


def _claude_tools() -> list[dict]:
    """Convert tool definitions to Claude format (input_schema)."""
    return [
        {
            "name": t["name"],
            "description": t["description"],
            "input_schema": t["parameters"],
        }
        for t in _TOOL_DEFINITIONS
    ]


def _openai_tools() -> list[dict]:
    """Convert tool definitions to OpenAI format ({type: function, function: ...})."""
    return [
        {
            "type": "function",
            "function": {
                "name": t["name"],
                "description": t["description"],
                "parameters": t["parameters"],
            },
        }
        for t in _TOOL_DEFINITIONS
    ]


def _gemini_tools() -> list[genai.types.Tool]:
    """Convert tool definitions to Gemini format (function_declarations)."""
    declarations = [
        genai.types.FunctionDeclaration(
            name=t["name"],
            description=t["description"],
            parameters=t["parameters"],
        )
        for t in _TOOL_DEFINITIONS
    ]
    return [genai.types.Tool(function_declarations=declarations)]


def _tool_call_description(name: str, arguments: dict) -> str:
    """Build a human-readable description for a tool call SSE event."""
    if name == "web_search":
        return f"Web Searching: {arguments.get('query', '')}"
    if name == "url_reader":
        return f"Reading: {arguments.get('url', '')}"
    return f"Calling: {name}"


# ── Provider-specific streaming with tool call loop ─────────────


async def _stream_chat_claude(
    messages: list[dict], model: str, identity_prompt: str
) -> AsyncGenerator[str | TokenUsage | ToolCallEvent, None]:
    client = _get_claude_client()
    system, chat_messages = _extract_system_and_messages(messages)
    if identity_prompt:
        system = f"{identity_prompt}\n{system}" if system else identity_prompt
    for msg in chat_messages:
        msg["content"] = _convert_to_claude_content(msg["content"])

    tools = _claude_tools()
    total_prompt_tokens = 0
    start_time = time.monotonic()

    for round_idx in range(_MAX_TOOL_ROUNDS):
        if time.monotonic() - start_time > _TOOL_LOOP_TIMEOUT:
            break

        if round_idx > 0:
            yield "\n\n"

        async with client.messages.stream(
            model=model,
            max_tokens=4096,
            system=system,
            messages=chat_messages,
            tools=tools,
        ) as stream:
            async for text in stream.text_stream:
                yield text
            final = await stream.get_final_message()

        round_usage = TokenUsage(
            prompt_tokens=final.usage.input_tokens,
            completion_tokens=final.usage.output_tokens,
        )

        # Collect tool_use blocks
        tool_use_blocks = [b for b in final.content if b.type == "tool_use"]

        if not tool_use_blocks:
            # Final round — no tool calls
            total_prompt_tokens += round_usage.prompt_tokens
            yield TokenUsage(
                prompt_tokens=total_prompt_tokens,
                completion_tokens=round_usage.completion_tokens,
            )
            return

        # Intermediate round — all tokens count as prompt
        total_prompt_tokens += round_usage.prompt_tokens + round_usage.completion_tokens

        # Limit tool calls per round
        tool_use_blocks = tool_use_blocks[:_MAX_TOOL_CALLS_PER_ROUND]

        # Reconstruct assistant message as dicts for next round
        assistant_content: list[dict] = []
        for block in final.content:
            if block.type == "text":
                assistant_content.append({"type": "text", "text": block.text})
            elif block.type == "tool_use":
                assistant_content.append({
                    "type": "tool_use",
                    "id": block.id,
                    "name": block.name,
                    "input": block.input,
                })
        chat_messages.append({"role": "assistant", "content": assistant_content})

        # Yield tool call events
        for block in tool_use_blocks:
            yield ToolCallEvent(content=_tool_call_description(block.name, block.input))

        # Execute tools in parallel
        results = await asyncio.gather(*[
            tool_executor.execute(block.name, block.input)
            for block in tool_use_blocks
        ])

        # Build tool_result message (all results in one user message for Claude)
        tool_results: list[dict] = [
            {"type": "tool_result", "tool_use_id": block.id, "content": result}
            for block, result in zip(tool_use_blocks, results)
        ]
        chat_messages.append({"role": "user", "content": tool_results})

    # Reached max rounds or timeout — do a final call WITHOUT tools
    # so the model summarises whatever tool results it already has.
    chat_messages.append({
        "role": "user",
        "content": (
            "You have reached the maximum number of tool-call rounds. "
            "Do NOT attempt any more tool calls. "
            "Summarise the information you have gathered so far and "
            "respond to the user directly in plain text."
        ),
    })
    yield "\n\n"
    async with client.messages.stream(
        model=model,
        max_tokens=4096,
        system=system,
        messages=chat_messages,
    ) as stream:
        async for text in stream.text_stream:
            yield text
        final = await stream.get_final_message()
    total_prompt_tokens += final.usage.input_tokens
    yield TokenUsage(
        prompt_tokens=total_prompt_tokens,
        completion_tokens=final.usage.output_tokens,
    )


async def _stream_chat_openai(
    messages: list[dict], model: str, provider: str, identity_prompt: str
) -> AsyncGenerator[str | TokenUsage | ToolCallEvent, None]:
    """Streaming with tool call loop for OpenAI-compatible providers (OpenAI, DeepSeek)."""
    client = _get_openai_client(provider)

    # Format initial messages
    if provider == "deepseek":
        oai_messages: list[dict] = [
            {"role": msg["role"], "content": _strip_image_blocks(msg["content"])}
            for msg in messages
        ]
    else:
        oai_messages = [
            {"role": msg["role"], "content": _convert_to_openai_content(msg["content"])}
            for msg in messages
        ]
    if identity_prompt:
        oai_messages = [{"role": "system", "content": identity_prompt}] + oai_messages

    tools = _openai_tools()
    total_prompt_tokens = 0
    start_time = time.monotonic()

    for round_idx in range(_MAX_TOOL_ROUNDS):
        if time.monotonic() - start_time > _TOOL_LOOP_TIMEOUT:
            break

        if round_idx > 0:
            yield "\n\n"

        accumulated_text = ""
        tool_calls_accum: dict[int, dict] = {}
        round_usage = TokenUsage(prompt_tokens=0, completion_tokens=0)

        stream = await client.chat.completions.create(
            model=model,
            messages=oai_messages,
            tools=tools,
            stream=True,
            stream_options={"include_usage": True},
        )

        async for chunk in stream:
            if chunk.choices:
                delta = chunk.choices[0].delta
                if delta.content:
                    accumulated_text += delta.content
                    yield delta.content
                if delta.tool_calls:
                    for tc_delta in delta.tool_calls:
                        idx = tc_delta.index
                        if idx not in tool_calls_accum:
                            tool_calls_accum[idx] = {"id": "", "name": "", "arguments": ""}
                        if tc_delta.id:
                            tool_calls_accum[idx]["id"] = tc_delta.id
                        if tc_delta.function and tc_delta.function.name:
                            tool_calls_accum[idx]["name"] = tc_delta.function.name
                        if tc_delta.function and tc_delta.function.arguments:
                            tool_calls_accum[idx]["arguments"] += tc_delta.function.arguments
            if chunk.usage:
                round_usage = TokenUsage(
                    prompt_tokens=chunk.usage.prompt_tokens,
                    completion_tokens=chunk.usage.completion_tokens,
                )

        if not tool_calls_accum:
            # Final round — no tool calls
            total_prompt_tokens += round_usage.prompt_tokens
            yield TokenUsage(
                prompt_tokens=total_prompt_tokens,
                completion_tokens=round_usage.completion_tokens,
            )
            return

        # Intermediate round — all tokens count as prompt
        total_prompt_tokens += round_usage.prompt_tokens + round_usage.completion_tokens

        # Parse accumulated tool calls
        parsed_tool_calls: list[dict] = []
        for idx in sorted(tool_calls_accum.keys()):
            tc = tool_calls_accum[idx]
            try:
                args = json.loads(tc["arguments"])
            except json.JSONDecodeError:
                args = {}
            parsed_tool_calls.append({"id": tc["id"], "name": tc["name"], "arguments": args})

        parsed_tool_calls = parsed_tool_calls[:_MAX_TOOL_CALLS_PER_ROUND]

        # Add assistant message with tool_calls
        assistant_msg: dict = {
            "role": "assistant",
            "content": accumulated_text or None,
            "tool_calls": [
                {
                    "id": tc["id"],
                    "type": "function",
                    "function": {
                        "name": tc["name"],
                        "arguments": json.dumps(tc["arguments"]),
                    },
                }
                for tc in parsed_tool_calls
            ],
        }
        oai_messages.append(assistant_msg)

        # Yield tool call events
        for tc in parsed_tool_calls:
            yield ToolCallEvent(content=_tool_call_description(tc["name"], tc["arguments"]))

        # Execute tools in parallel
        results = await asyncio.gather(*[
            tool_executor.execute(tc["name"], tc["arguments"])
            for tc in parsed_tool_calls
        ])

        # Add tool result messages (one per tool for OpenAI format)
        for tc, result in zip(parsed_tool_calls, results):
            oai_messages.append({
                "role": "tool",
                "tool_call_id": tc["id"],
                "content": result,
            })

    # Reached max rounds or timeout — do a final call WITHOUT tools
    # so the model summarises whatever tool results it already has.
    # Add an explicit instruction so the model doesn't hallucinate tool calls as text.
    oai_messages.append({
        "role": "user",
        "content": (
            "You have reached the maximum number of tool-call rounds. "
            "Do NOT attempt any more tool calls. "
            "Summarise the information you have gathered so far and "
            "respond to the user directly in plain text."
        ),
    })
    yield "\n\n"
    final_stream = await client.chat.completions.create(
        model=model,
        messages=oai_messages,
        stream=True,
        stream_options={"include_usage": True},
    )
    final_usage = TokenUsage(prompt_tokens=0, completion_tokens=0)
    async for chunk in final_stream:
        if chunk.choices and chunk.choices[0].delta.content:
            yield chunk.choices[0].delta.content
        if chunk.usage:
            final_usage = TokenUsage(
                prompt_tokens=chunk.usage.prompt_tokens,
                completion_tokens=chunk.usage.completion_tokens,
            )
    total_prompt_tokens += final_usage.prompt_tokens
    yield TokenUsage(
        prompt_tokens=total_prompt_tokens,
        completion_tokens=final_usage.completion_tokens,
    )


async def _stream_chat_gemini(
    messages: list[dict], model: str, identity_prompt: str
) -> AsyncGenerator[str | TokenUsage | ToolCallEvent, None]:
    """Gemini streaming chat with tool-call loop.

    Uses streaming for text generation and non-streaming for tool-call rounds
    (to reliably capture function_call parts).
    """
    client = _get_gemini_client()
    system, chat_messages = _extract_system_and_messages(messages)
    if identity_prompt:
        system = f"{identity_prompt}\n{system}" if system else identity_prompt

    # Build Gemini contents list
    contents: list[genai.types.Content] = []
    for msg in chat_messages:
        role = "user" if msg["role"] == "user" else "model"
        contents.append(genai.types.Content(
            role=role,
            parts=_convert_to_gemini_parts(msg["content"]),
        ))

    config = genai.types.GenerateContentConfig(
        system_instruction=system if system else None,
        tools=_gemini_tools(),
        automatic_function_calling=genai.types.AutomaticFunctionCallingConfig(
            disable=True,
        ),
    )

    total_prompt_tokens = 0
    start_time = time.monotonic()

    for round_idx in range(_MAX_TOOL_ROUNDS):
        if time.monotonic() - start_time > _TOOL_LOOP_TIMEOUT:
            break

        if round_idx > 0:
            yield "\n\n"

        # Use streaming for every round so the user sees tokens in real time
        # (especially important when round 0 is a tool call and text comes in round 1+).
        accumulated_text = ""
        function_call_parts: list[genai.types.Part] = []
        round_usage = TokenUsage(prompt_tokens=0, completion_tokens=0)

        stream = await client.aio.models.generate_content_stream(
            model=model,
            contents=contents,
            config=config,
        )
        async for chunk in stream:
            if chunk.usage_metadata:
                prompt = chunk.usage_metadata.prompt_token_count or 0
                completion = chunk.usage_metadata.candidates_token_count or 0
                if prompt > round_usage.prompt_tokens:
                    round_usage = TokenUsage(prompt_tokens=prompt, completion_tokens=completion)
                else:
                    round_usage = TokenUsage(
                        prompt_tokens=round_usage.prompt_tokens,
                        completion_tokens=max(round_usage.completion_tokens, completion),
                    )

            if chunk.candidates:
                for candidate in chunk.candidates:
                    if candidate.content and candidate.content.parts:
                        for part in candidate.content.parts:
                            if part.function_call:
                                function_call_parts.append(part)
                            elif part.text:
                                accumulated_text += part.text
                                yield part.text

        if not function_call_parts:
            # No tool calls — done.
            total_prompt_tokens += round_usage.prompt_tokens
            yield TokenUsage(
                prompt_tokens=total_prompt_tokens,
                completion_tokens=round_usage.completion_tokens,
            )
            return

        # ── Tool call round ──
        total_prompt_tokens += round_usage.prompt_tokens + round_usage.completion_tokens

        function_call_parts = function_call_parts[:_MAX_TOOL_CALLS_PER_ROUND]

        # Yield tool call events
        for p in function_call_parts:
            fc = p.function_call
            args = dict(fc.args) if fc.args else {}
            yield ToolCallEvent(content=_tool_call_description(fc.name, args))

        # Add assistant response to contents (text + function calls)
        assistant_parts: list[genai.types.Part] = []
        if accumulated_text:
            assistant_parts.append(genai.types.Part(text=accumulated_text))
        assistant_parts.extend(function_call_parts)
        contents.append(genai.types.Content(role="model", parts=assistant_parts))

        # Execute tools in parallel
        results = await asyncio.gather(*[
            tool_executor.execute(p.function_call.name, dict(p.function_call.args) if p.function_call.args else {})
            for p in function_call_parts
        ])

        # Add tool results as user message
        tool_result_parts = [
            genai.types.Part(
                function_response=genai.types.FunctionResponse(
                    name=p.function_call.name,
                    response={"result": result},
                )
            )
            for p, result in zip(function_call_parts, results)
        ]
        contents.append(genai.types.Content(role="user", parts=tool_result_parts))

    # Reached max rounds or timeout — do a final call WITHOUT tools
    # so the model summarises whatever tool results it already has.
    contents.append(genai.types.Content(
        role="user",
        parts=[genai.types.Part(text=(
            "You have reached the maximum number of tool-call rounds. "
            "Do NOT attempt any more tool calls. "
            "Summarise the information you have gathered so far and "
            "respond to the user directly in plain text."
        ))],
    ))
    yield "\n\n"
    no_tools_config = genai.types.GenerateContentConfig(
        system_instruction=system if system else None,
    )
    stream = await client.aio.models.generate_content_stream(
        model=model,
        contents=contents,
        config=no_tools_config,
    )
    final_usage = TokenUsage(prompt_tokens=0, completion_tokens=0)
    async for chunk in stream:
        if chunk.usage_metadata:
            p = chunk.usage_metadata.prompt_token_count or 0
            c = chunk.usage_metadata.candidates_token_count or 0
            final_usage = TokenUsage(prompt_tokens=max(final_usage.prompt_tokens, p), completion_tokens=max(final_usage.completion_tokens, c))
        if chunk.candidates:
            for candidate in chunk.candidates:
                if candidate.content and candidate.content.parts:
                    for part in candidate.content.parts:
                        if part.text:
                            yield part.text
    total_prompt_tokens += final_usage.prompt_tokens
    yield TokenUsage(
        prompt_tokens=total_prompt_tokens,
        completion_tokens=final_usage.completion_tokens,
    )


# ── Public entry point ──────────────────────────────────────────


async def stream_chat(
    messages: list[dict], model_index: int
) -> AsyncGenerator[str | TokenUsage | ToolCallEvent, None]:
    """Stream chat completion tokens with tool call support.

    Yields:
        str: content delta tokens
        ToolCallEvent: when a tool is being executed (for SSE forwarding)
        TokenUsage: final token usage (always the last item yielded)
    """
    provider, model = _resolve_model(model_index)
    identity_prompt = _get_system_prompt(provider)

    if provider == "claude":
        gen = _stream_chat_claude(messages, model, identity_prompt)
    elif provider == "gemini":
        gen = _stream_chat_gemini(messages, model, identity_prompt)
    else:
        gen = _stream_chat_openai(messages, model, provider, identity_prompt)

    async for item in gen:
        yield item
