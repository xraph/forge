import type { DecodedFrame, FrameDecoder } from './live';

/**
 * The transport kinds `extensions/streaming` reserves in its `type` field.
 *
 * Copied from the `MessageType*` constants in
 * `extensions/streaming/internal/streaming.go`. The Go half pins this exact set
 * in `extensions/streaming/frame_test.go`, and that test names this file: an
 * eighth kind added there fails there, rather than arriving here as a frame name
 * no binding can ever claim and being reported as an unknown message forever.
 */
const TRANSPORT_KINDS: ReadonlySet<string> = new Set([
  'message',
  'presence',
  'typing',
  'system',
  'join',
  'leave',
  'error',
]);

export interface ForgeStreamingDecoderOptions {
  /**
   * Map the envelope's `channel_id` onto a manifest channel.
   *
   * Unset by default, and the default is the answer for every application whose
   * sockets are one-per-channel -- which is what the generated clients produce,
   * since a `WebSocketClient` is constructed per path. See the note on
   * `forgeStreamingDecoder` for why surfacing the raw id would be actively
   * wrong there.
   *
   * Return `undefined` for an id this mapping does not recognise, which leaves
   * the frame keyed on the channel it arrived on. That is the safe direction:
   * an unrecognised id falling back to the arrival channel decodes correctly on
   * a single-channel socket, whereas a guessed channel is a lookup miss.
   */
  readonly channelOf?: (channelID: string) => string | undefined;
}

/**
 * The envelope reader for the Forge streaming extension.
 *
 * The default `decodeFrame` cannot read this envelope, and the reason is a
 * genuine collision rather than an oversight. `decodeFrame` resolves the frame
 * name as `type ?? event ?? name`, because in the shapes it was written for
 * `type` *is* the message name -- `{type: 'order.created', payload}` is what a
 * plain Forge WebSocket handler emits. In the streaming extension `type` means
 * something else entirely: it is the transport kind, one of seven reserved
 * strings, and the domain event name lives in `event`. So `type` wins, every
 * frame decodes as `message`, no manifest row is keyed on `message`, and the
 * entire channel lands in `onUnknown`. Both readings are correct for their own
 * envelope and neither can be made correct for both, which is exactly the split
 * the injectable `FrameDecoder` exists for.
 *
 * Three decisions worth stating, because each had a cheaper wrong version:
 *
 * **`event` first, `type` as the fallback.** The fallback is not defensive
 * padding: it is what keeps this decoder a superset of the default one, so an
 * application that mixes streaming-extension channels with hand-rolled
 * `{type: 'order.created'}` handlers can install this decoder globally instead
 * of routing two decoders by endpoint. Reading `event` alone would have been
 * one line shorter and would have quietly broken the hand-rolled half.
 *
 * **A reserved kind reached through the fallback is dropped, not reported.**
 * With no `event`, a presence or typing or join frame resolves to its transport
 * kind, and no generated manifest binds those -- `writeStreams` emits one row
 * per AsyncAPI domain message, and `presence` is not one. Passing them through
 * would mean every channel logs a handful of unknown-message warnings on
 * connect, for frames that are working exactly as designed; `FrameDecoder`'s
 * own contract says `undefined` is how a transport-level frame is dropped
 * without a warning, and this is that case. The drop is deliberately narrow:
 * it applies only to the seven reserved strings, and only when they were
 * reached through the fallback. An `event` naming a domain message is always
 * honoured, even on a frame whose `type` is `system`.
 *
 * **`channel_id` is not surfaced as the channel unless asked.** This is the
 * trap. `DecodedFrame.channel` is not an annotation -- `StreamBinder.accept`
 * takes it as an *override* of the channel the frame arrived on -- and the two
 * fields are not the same kind of name. A manifest channel is the endpoint path
 * the socket is served on (`/ws/orders`, as `writeStreams` emits from
 * `ch.path`); the extension's `channel_id` is a logical subscription id
 * (`orders`). Returning the id verbatim would replace a key that matches a
 * binding with one that matches nothing, turning a decoder fix into a lookup
 * regression on precisely the applications it was meant to repair. Omitting it
 * lets `accept` fall back to the arrival channel, which is the right answer
 * whenever a socket carries one channel. An application that genuinely
 * multiplexes several channels over one endpoint, and binds the same message
 * name on more than one of them, passes `channelOf` and gets the
 * disambiguation -- it is the only party that knows how its ids map to paths.
 *
 * Nothing here reads the AsyncAPI `name` spelling that `decodeFrame` accepts as
 * its third candidate. The streaming extension does not emit it, and adding a
 * branch for a field this server never sends would only create a way for the
 * reserved-kind drop above to be silently bypassed.
 */
export function forgeStreamingDecoder(options: ForgeStreamingDecoderOptions = {}): FrameDecoder {
  const channelOf = options.channelOf;

  return (message: unknown): DecodedFrame | undefined => {
    if (message === null || typeof message !== 'object') return undefined;

    const envelope = message as Record<string, unknown>;
    const event = envelope['event'];

    let name: string;

    if (typeof event === 'string' && event !== '') {
      name = event;
    } else {
      const kind = envelope['type'];

      if (typeof kind !== 'string' || kind === '') return undefined;

      // A transport frame: it carries no domain event, so there is nothing a
      // binding could be keyed on. See the note above on why this is a silent
      // drop rather than an unknown message.
      if (TRANSPORT_KINDS.has(kind)) return undefined;

      name = kind;
    }

    // `data` is the streaming extension's payload field; `payload` is checked
    // first for the same reason the fallback above exists, and costs nothing --
    // no envelope carries both. An envelope with neither is its own payload,
    // which is what a server sending the entity flat produces.
    const payload =
      'payload' in envelope ? envelope['payload'] : 'data' in envelope ? envelope['data'] : envelope;

    if (channelOf === undefined) return { message: name, payload };

    const channelID = envelope['channel_id'] ?? envelope['channel'];

    if (typeof channelID !== 'string' || channelID === '') return { message: name, payload };

    const channel = channelOf(channelID);

    return typeof channel === 'string' && channel !== ''
      ? { message: name, payload, channel }
      : { message: name, payload };
  };
}
