// @vitest-environment jsdom
import { cleanup, fireEvent, render } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { MonitorList } from "./monitor-list";

afterEach(cleanup);
const monitor = { id: "one", url: "https://one.example", interval_seconds: 60, created_at: "2026-10-06" };
const favicon = "data:image/png;base64,aGVsbG8=";
const props = { disabled: false, onEdit: () => {}, onDelete: () => {} };

it("shows a globe when an icon is absent or fails to decode", () => {
  const { container, rerender } = render(<MonitorList {...props} monitors={[monitor]} />);
  expect(container.querySelector("svg")).not.toBeNull();
  rerender(<MonitorList {...props} monitors={[{ ...monitor, favicon }]} />);
  const image = container.querySelector("img")!;
  expect(image.getAttribute("src")).toBe(favicon);
  expect(image.getAttribute("alt")).toBe("");
  fireEvent.error(image);
  expect(container.querySelector("img")).toBeNull();
  expect(container.querySelector("svg")).not.toBeNull();
  rerender(<MonitorList {...props} monitors={[{ ...monitor, url: "https://two.example", favicon }]} />);
  expect(container.querySelector("img")).not.toBeNull();
  rerender(<MonitorList {...props} monitors={[{ ...monitor, url: "https://three.example" }]} />);
  expect(container.querySelector("img")).toBeNull();
});
