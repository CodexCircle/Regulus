import type { SocketLike } from "./LiveProvider";

// FakeSocket stands in for WebSocket in tests. Tests drive it by hand.
export class FakeSocket implements SocketLike {
  static instances: FakeSocket[] = [];

  static reset() {
    FakeSocket.instances = [];
  }

  static get last(): FakeSocket {
    return FakeSocket.instances[FakeSocket.instances.length - 1];
  }

  onopen: (() => void) | null = null;
  onmessage: ((event: { data: unknown }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;

  constructor(readonly url: string) {
    FakeSocket.instances.push(this);
  }

  close() {
    this.closed = true;
    this.onclose?.();
  }

  // Test helpers: the server accepts, sends a message, or drops the connection.
  open() {
    this.onopen?.();
  }

  send(message: unknown) {
    this.onmessage?.({ data: JSON.stringify(message) });
  }

  drop() {
    this.onclose?.();
  }
}
