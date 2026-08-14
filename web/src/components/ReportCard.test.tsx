import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Post, Report } from "../api/types";
import ReportCard from "./ReportCard";

/**
 * The queue is where a volunteer moderator decides what to do about one member
 * on the word of another, and it used to name both of them with eight
 * characters of a uuid. The names are on this read and this read only — the
 * report echoed back to its own filer carries none — so these pin that the card
 * uses them, and that it still says something when there is no name to use.
 */

const AUTHOR = "0193a7b2-aaaa-7000-8000-000000000000";
const REPORTER = "0193c1d4-bbbb-7000-8000-000000000000";

function report(overrides: Partial<Report> = {}): Report {
  return {
    id: "report-1",
    reporter_id: REPORTER,
    post_id: "post-1",
    reason: "Spam content",
    status: "pending",
    created_at: "2026-03-01T12:00:00Z",
    ...overrides,
  };
}

function post(overrides: Partial<Post> = {}): Post {
  return {
    id: "post-1",
    author_id: AUTHOR,
    body: "Something the town did not care for",
    image_path: "",
    status: "visible",
    created_at: "2026-03-01T11:00:00Z",
    edited_at: null,
    ...overrides,
  };
}

/** The card fetches the reported post itself; this is the only request it makes. */
function stubPost(body: Post) {
  vi.stubGlobal(
    "fetch",
    vi.fn(() =>
      Promise.resolve({
        ok: true,
        status: 200,
        json: () => Promise.resolve(body),
      } as unknown as Response),
    ),
  );
}

function renderCard(r: Report, p: Post) {
  stubPost(p);
  return render(
    <MemoryRouter>
      <ReportCard
        report={r}
        currentUserId="mod-1"
        onDismiss={vi.fn()}
        onTakeAction={vi.fn()}
        onRemovePost={vi.fn()}
      />
    </MemoryRouter>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("ReportCard naming", () => {
  it("names the author of the reported post", async () => {
    renderCard(report(), post({ author_display_name: "Ada Lovelace" }));
    expect(await screen.findByText("Ada Lovelace")).toBeInTheDocument();
  });

  it("names whoever filed the report", async () => {
    renderCard(report({ reporter_display_name: "Grace Hopper" }), post());
    expect(await screen.findByText(/Reporter: Grace Hopper/)).toBeInTheDocument();
  });

  // The key is omitted rather than sent empty for a member with no display
  // name, and the row still has to identify them: the name is missing, not the
  // report.
  it("falls back to the id prefix for a reporter with no name", async () => {
    renderCard(report(), post());
    expect(await screen.findByText(/Reporter: 0193c1d4\.\.\./)).toBeInTheDocument();
  });

  it("falls back to the id prefix for an author with no name", async () => {
    renderCard(report(), post());
    await waitFor(() => expect(screen.getByText("0193a7b2...")).toBeInTheDocument());
  });

  // The name is for reading; the id is what the link is built from, and a
  // moderator following it must still land on the right history.
  it("still links to the author's history by id", async () => {
    renderCard(report(), post({ author_display_name: "Ada Lovelace" }));
    expect(await screen.findByRole("link", { name: "View history" })).toHaveAttribute(
      "href",
      `/moderation/users/${AUTHOR}`,
    );
  });
});
