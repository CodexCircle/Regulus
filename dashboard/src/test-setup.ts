import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, beforeEach, vi } from "vitest";
import { FakeSocket } from "./fake-socket";

// Vitest globals are off, so Testing Library cannot register its own cleanup.
afterEach(cleanup);

// No test may open a real connection: the live stream is a fake that does
// nothing unless a test drives it.
beforeEach(() => {
  FakeSocket.reset();
  vi.stubGlobal("WebSocket", FakeSocket);
});
