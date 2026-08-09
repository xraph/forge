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
   * Consulted for `channel_id` and for nothing else. An envelope that states a
   * literal `channel` is already naming an endpoint path, so it bypasses this
   * mapping entirely -- asking a mapping written for logical ids about a path
   * gets `undefined` back and would throw away an override the frame carried.
   *
   * Return `undefined` for an id this mapping does not recognise, which leaves
   * the frame keyed on a literal `channel` if the envelope has one and on the
   * channel it arrived on otherwise. That is the safe direction: an
   * unrecognised id falling back decodes correctly on a single-channel socket,
   * whereas a guessed channel is a lookup miss.
   */
  readonly channelOf?: (channelID: string) => string | undefined;
}

/**
 * The envelope reader for the Forge streaming extension.
 *
 * The default `decodeFrame` reads this envelope's *name* correctly: it resolves
 * the frame name as `event`, then `type`, then `name`, and the streaming
 * extension puts the domain name in `event`. That was not always so. The order
 * used to be `type ?? event ?? name`, because in the shapes `decodeFrame` was
 * written for `type` *is* the message name -- `{type: 'order.created', payload}`
 * is what a plain Forge WebSocket handler emits -- and in this extension `type`
 * means something else entirely: the transport kind, one of seven reserved
 * strings. So `type` won, every frame decoded as `message`, no manifest row is
 * keyed on `message`, and the entire channel landed in `onUnknown`. Reading
 * `event` first fixed that in the default, for everybody, and this decoder no
 * longer exists to make the name work.
 *
 * It exists for the two things the default has no business guessing. First, it
 * knows which names are reserved transport kinds and drops those frames
 * silently instead of reporting them as unknown messages -- a policy that
 * depends on knowing this specific server's `type` vocabulary. Second, it owns
 * the `channel_id` mapping, which only an application can supply. Everything
 * else about it is kept deliberately identical to the default so that
 * installing it globally is safe; see the channel note below, which is the part
 * that is easiest to get subtly wrong.
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
 * **`channel_id` is not surfaced as the channel unless asked; `channel` always
 * is.** This is the trap, and the distinction between the two fields is the
 * whole of it. `DecodedFrame.channel` is not an annotation -- `StreamBinder.accept`
 * takes it as an *override* of the channel the frame arrived on -- so whatever
 * goes in it must be the kind of name a binding is keyed on. A manifest channel
 * is the endpoint path the socket is served on (`/ws/orders`, as `writeStreams`
 * emits from `ch.path`), and an envelope's `channel` field is already that; the
 * extension's `channel_id` is a logical subscription id (`orders`), which is
 * not. Surfacing the id verbatim would replace a key that matches a binding
 * with one that matches nothing, turning a decoder fix into a lookup regression
 * on precisely the applications it was meant to repair. Omitting it lets
 * `accept` fall back to the arrival channel, which is the right answer whenever
 * a socket carries one channel. An application that genuinely multiplexes
 * several channels over one endpoint, and binds the same message name on more
 * than one of them, passes `channelOf` and gets the disambiguation -- it is the
 * only party that knows how its ids map to paths.
 *
 * A literal `channel` is passed straight through, with or without `channelOf`,
 * exactly as `decodeFrame` does. Dropping it was the original spelling and it
 * quietly made this decoder a *subset* of the default for anyone whose sockets
 * are multiplexed: `SubscriptionManager.deliver` fans every socket message to
 * every channel registered on that socket, so a hand-rolled `{type, channel,
 * payload}` frame that used to bind on exactly the channel it named was instead
 * looked up once per channel on the endpoint -- matching the wrong binding
 * wherever the name is bound twice, and reporting an unknown message on every
 * channel that does not bind it. That is a real regression for an application
 * following the README's advice to install this decoder globally.
 *
 * `channelOf` is consulted for `channel_id` and never for `channel`. Routing a
 * path through a mapping whose documented domain is logical ids asks the
 * application a question about a value it has no ids for; it answers
 * `undefined`, and the override the envelope explicitly carried is discarded.
 * The rule that falls out is the only coherent one: each field is treated as
 * the kind of name it is. A recognised `channel_id` wins over a literal
 * `channel`, because a mapping is something the application went out of its way
 * to supply for exactly this disambiguation; an unrecognised or absent one
 * falls through to the literal, and then to the arrival channel.
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

    // `channel` is already an endpoint path, which is what a binding is keyed
    // on, so it needs no mapping and gets none. Passing it through is what
    // keeps this decoder a superset of the default for the channel as well as
    // for the name.
    const stated = envelope['channel'];
    const path = typeof stated === 'string' && stated !== '' ? stated : undefined;

    if (channelOf === undefined) {
      return path === undefined
        ? { message: name, payload }
        : { message: name, payload, channel: path };
    }

    // `channel_id` is a logical id, so it is the only field the mapping is
    // asked about. An id it does not recognise -- or an absent one -- leaves
    // the stated path standing rather than cancelling it.
    const id = envelope['channel_id'];
    const mapped = typeof id === 'string' && id !== '' ? channelOf(id) : undefined;
    const channel = typeof mapped === 'string' && mapped !== '' ? mapped : path;

    return channel === undefined
      ? { message: name, payload }
      : { message: name, payload, channel };
  };
}
