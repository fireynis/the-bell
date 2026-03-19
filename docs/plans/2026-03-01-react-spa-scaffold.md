# React SPA Scaffold Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create a Vite + React + TypeScript + Tailwind CSS frontend scaffold in `web/` with routing, an API client wrapper, and dev proxy configuration.

**Architecture:** Single-page app (React 19 + TypeScript) built with Vite 6, styled with Tailwind CSS v4, routed with React Router v7. The SPA communicates with the Go API via `/api/v1/*` and with Kratos directly via `/.ory/*`. Session authentication uses the `bell_session` cookie (SameSite=Lax, set by Kratos). In production, the SPA is embedded in the Go binary via the multi-stage Dockerfile.

**Tech Stack:** Vite 6, React 19, TypeScript, Tailwind CSS v4 (`@tailwindcss/vite`), React Router v7

**Beads task:** `the-bell-cvs.1`

**Key context:**
- `web/` exists with only `.gitkeep`
- Dockerfile expects: `web/package.json`, `web/package-lock.json`, `npm run build` → `web/dist/`
- Kratos CORS allows `http://localhost:5173` and `http://bell.home.arpa`
- Kratos UI URLs: `/auth/login`, `/auth/registration`, `/auth/settings`, `/auth/recovery`, `/auth/verification`
- Go API runs on port 8080, Kratos public on 4433
- Session cookie: `bell_session`

---

### Task 1: Scaffold Vite + React + TypeScript project

**Files:**
- Create: `web/package.json`, `web/tsconfig.json`, `web/tsconfig.app.json`, `web/tsconfig.node.json`, `web/vite.config.ts`, `web/index.html`, `web/src/main.tsx`, `web/src/App.tsx`, `web/src/vite-env.d.ts`
- Delete: `web/.gitkeep`

**Step 1: Create Vite project**

Run from repo root:
```bash
cd web && npm create vite@latest . -- --template react-ts
```

This overwrites `.gitkeep` and creates the standard Vite React-TS template files.

**Step 2: Install dependencies**

```bash
cd web && npm install
```

**Step 3: Verify it builds**

```bash
cd web && npm run build
```

Expected: Builds successfully, creates `web/dist/` directory.

**Step 4: Verify dev server starts**

```bash
cd web && npx vite --host 0.0.0.0 &
sleep 3
curl -s http://localhost:5173 | head -5
kill %1
```

Expected: Returns HTML content.

**Step 5: Clean up and commit**

Remove the default Vite boilerplate content (counter, logos, CSS) — we'll replace it in later tasks. Keep the structural files.

Replace `web/src/App.tsx`:
```tsx
function App() {
  return <h1>The Bell</h1>;
}

export default App;
```

Remove `web/src/App.css` and `web/src/index.css` (will be replaced by Tailwind).

Update `web/src/main.tsx` to remove the CSS import:
```tsx
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
```

Delete: `web/src/assets/react.svg`, `web/public/vite.svg`

```bash
git add web/
git commit -m "feat(web): scaffold Vite + React + TypeScript project"
```

---

### Task 2: Add Tailwind CSS v4

**Files:**
- Modify: `web/package.json` (new deps)
- Modify: `web/vite.config.ts` (add plugin)
- Create: `web/src/index.css` (Tailwind import)
- Modify: `web/src/main.tsx` (import CSS)

**Step 1: Install Tailwind CSS v4**

```bash
cd web && npm install -D tailwindcss @tailwindcss/vite
```

**Step 2: Add Tailwind Vite plugin**

Update `web/vite.config.ts`:
```ts
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
});
```

**Step 3: Create CSS entry point**

Create `web/src/index.css`:
```css
@import "tailwindcss";
```

**Step 4: Import CSS in main.tsx**

Update `web/src/main.tsx` to add the CSS import:
```tsx
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./index.css";
import App from "./App";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
```

**Step 5: Verify Tailwind works**

Update `web/src/App.tsx` to use a Tailwind class:
```tsx
function App() {
  return <h1 className="text-3xl font-bold text-blue-600">The Bell</h1>;
}

export default App;
```

```bash
cd web && npm run build
```

Expected: Builds successfully. The output CSS should contain Tailwind utility classes.

**Step 6: Commit**

```bash
git add web/
git commit -m "feat(web): add Tailwind CSS v4"
```

---

### Task 3: Add React Router with route structure

**Files:**
- Modify: `web/package.json` (new dep)
- Create: `web/src/routes.tsx`
- Create: `web/src/pages/Home.tsx`
- Create: `web/src/pages/auth/Login.tsx`
- Create: `web/src/pages/auth/Registration.tsx`
- Create: `web/src/pages/auth/Settings.tsx`
- Create: `web/src/pages/auth/Recovery.tsx`
- Create: `web/src/pages/auth/Verification.tsx`
- Create: `web/src/pages/NotFound.tsx`
- Modify: `web/src/App.tsx`

**Step 1: Install React Router**

```bash
cd web && npm install react-router
```

**Step 2: Create placeholder page components**

Each page is a minimal placeholder. These will be fleshed out in later tasks.

Create `web/src/pages/Home.tsx`:
```tsx
export default function Home() {
  return <div className="p-4"><h1 className="text-2xl font-bold">Feed</h1></div>;
}
```

Create `web/src/pages/auth/Login.tsx`:
```tsx
export default function Login() {
  return <div className="p-4"><h1 className="text-2xl font-bold">Login</h1></div>;
}
```

Create `web/src/pages/auth/Registration.tsx`:
```tsx
export default function Registration() {
  return <div className="p-4"><h1 className="text-2xl font-bold">Register</h1></div>;
}
```

Create `web/src/pages/auth/Settings.tsx`:
```tsx
export default function Settings() {
  return <div className="p-4"><h1 className="text-2xl font-bold">Settings</h1></div>;
}
```

Create `web/src/pages/auth/Recovery.tsx`:
```tsx
export default function Recovery() {
  return <div className="p-4"><h1 className="text-2xl font-bold">Account Recovery</h1></div>;
}
```

Create `web/src/pages/auth/Verification.tsx`:
```tsx
export default function Verification() {
  return <div className="p-4"><h1 className="text-2xl font-bold">Email Verification</h1></div>;
}
```

Create `web/src/pages/NotFound.tsx`:
```tsx
import { Link } from "react-router";

export default function NotFound() {
  return (
    <div className="p-4 text-center">
      <h1 className="text-2xl font-bold">404</h1>
      <p className="mt-2 text-gray-600">Page not found</p>
      <Link to="/" className="mt-4 inline-block text-blue-600 hover:underline">
        Go home
      </Link>
    </div>
  );
}
```

**Step 3: Create route configuration**

Create `web/src/routes.tsx`:
```tsx
import { type RouteObject } from "react-router";
import Home from "./pages/Home";
import Login from "./pages/auth/Login";
import Registration from "./pages/auth/Registration";
import Settings from "./pages/auth/Settings";
import Recovery from "./pages/auth/Recovery";
import Verification from "./pages/auth/Verification";
import NotFound from "./pages/NotFound";

export const routes: RouteObject[] = [
  { path: "/", element: <Home /> },
  { path: "/auth/login", element: <Login /> },
  { path: "/auth/registration", element: <Registration /> },
  { path: "/auth/settings", element: <Settings /> },
  { path: "/auth/recovery", element: <Recovery /> },
  { path: "/auth/verification", element: <Verification /> },
  { path: "*", element: <NotFound /> },
];
```

**Step 4: Wire router into App**

Update `web/src/App.tsx`:
```tsx
import { createBrowserRouter, RouterProvider } from "react-router";
import { routes } from "./routes";

const router = createBrowserRouter(routes);

export default function App() {
  return <RouterProvider router={router} />;
}
```

**Step 5: Verify build**

```bash
cd web && npm run build
```

Expected: Builds successfully.

**Step 6: Commit**

```bash
git add web/
git commit -m "feat(web): add React Router with auth and feed routes"
```

---

### Task 4: Create API client wrapper

**Files:**
- Create: `web/src/api/client.ts`
- Create: `web/src/api/types.ts`

The API client wraps `fetch` with:
- `credentials: "include"` on every request (sends `bell_session` cookie)
- JSON content-type headers
- Typed response handling
- Error types matching the Go API

**Step 1: Create API types**

Create `web/src/api/types.ts`:
```ts
export interface Post {
  id: string;
  author_id: string;
  body: string;
  image_path: string;
  status: string;
  created_at: string;
  edited_at: string | null;
}

export interface User {
  id: string;
  display_name: string;
  bio: string;
  avatar_url: string;
  trust_score: number;
  role: string;
  is_active: boolean;
  joined_at: string;
}

export interface ApiError {
  error: string;
  status: number;
}
```

**Step 2: Create API client**

Create `web/src/api/client.ts`:
```ts
import type { ApiError } from "./types";

class ApiClient {
  private baseUrl: string;

  constructor(baseUrl = "/api/v1") {
    this.baseUrl = baseUrl;
  }

  async request<T>(path: string, options: RequestInit = {}): Promise<T> {
    const url = `${this.baseUrl}${path}`;
    const res = await fetch(url, {
      ...options,
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
        ...options.headers,
      },
    });

    if (!res.ok) {
      const body = await res.json().catch(() => ({ error: res.statusText }));
      throw { error: body.error ?? res.statusText, status: res.status } satisfies ApiError;
    }

    if (res.status === 204) return undefined as T;
    return res.json();
  }

  get<T>(path: string): Promise<T> {
    return this.request<T>(path);
  }

  post<T>(path: string, body: unknown): Promise<T> {
    return this.request<T>(path, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  patch<T>(path: string, body: unknown): Promise<T> {
    return this.request<T>(path, {
      method: "PATCH",
      body: JSON.stringify(body),
    });
  }

  delete(path: string): Promise<void> {
    return this.request<void>(path, { method: "DELETE" });
  }
}

export const api = new ApiClient();
```

**Step 3: Verify build**

```bash
cd web && npm run build
```

Expected: Builds successfully. The API client is importable but not yet used.

**Step 4: Commit**

```bash
git add web/src/api/
git commit -m "feat(web): add typed API client with session cookie credentials"
```

---

### Task 5: Configure Vite dev proxy

**Files:**
- Modify: `web/vite.config.ts`

The dev proxy routes:
- `/api/*` → Go server at `http://localhost:8080` (API endpoints)
- `/.ory/*` → Kratos public at `http://localhost:4433` (self-service flows)

This avoids CORS issues during local development — the browser sees everything on `localhost:5173`.

**Step 1: Add proxy configuration**

Update `web/vite.config.ts`:
```ts
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      "/api": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
      "/.ory": {
        target: "http://localhost:4433",
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/.ory/, ""),
      },
    },
  },
});
```

Note: The `/.ory` rewrite strips the prefix so `/.ory/self-service/login/flows` becomes `/self-service/login/flows` on Kratos.

**Step 2: Verify build**

```bash
cd web && npm run build
```

Expected: Builds successfully. Proxy only affects dev server, not production builds.

**Step 3: Commit**

```bash
git add web/vite.config.ts
git commit -m "feat(web): configure Vite dev proxy for API and Kratos"
```

---

### Task 6: TypeScript strict mode and linting

**Files:**
- Modify: `web/tsconfig.app.json` (ensure strict mode)
- Verify: `web/tsconfig.json`, `web/tsconfig.node.json`

**Step 1: Verify TypeScript strict mode is enabled**

The Vite template should already have `"strict": true` in `tsconfig.app.json`. Verify this is the case. If not, enable it.

**Step 2: Run type check**

```bash
cd web && npx tsc --noEmit
```

Expected: No type errors.

**Step 3: Commit (if changes needed)**

```bash
git add web/tsconfig*.json
git commit -m "chore(web): ensure strict TypeScript configuration"
```

---

### Task 7: Final verification and cleanup

**Files:**
- Verify: All `web/` files

**Step 1: Clean build**

```bash
cd web && rm -rf dist node_modules && npm ci && npm run build
```

Expected: Clean install and build succeeds.

**Step 2: Verify Dockerfile still works (optional)**

The Dockerfile expects `web/package.json`, `web/package-lock.json`, and `npm run build` → `dist/`. This should work with the scaffold. No changes needed to the Dockerfile.

**Step 3: Verify directory structure**

```bash
find web/ -type f | head -30
```

Expected structure:
```
web/
├── index.html
├── package.json
├── package-lock.json
├── tsconfig.json
├── tsconfig.app.json
├── tsconfig.node.json
├── vite.config.ts
├── public/
├── src/
│   ├── main.tsx
│   ├── App.tsx
│   ├── index.css
│   ├── vite-env.d.ts
│   ├── api/
│   │   ├── client.ts
│   │   └── types.ts
│   ├── pages/
│   │   ├── Home.tsx
│   │   ├── NotFound.tsx
│   │   └── auth/
│   │       ├── Login.tsx
│   │       ├── Registration.tsx
│   │       ├── Settings.tsx
│   │       ├── Recovery.tsx
│   │       └── Verification.tsx
│   └── routes.tsx
```

**Step 4: Commit any final cleanup**

```bash
git add web/
git commit -m "chore(web): final scaffold cleanup"
```

---

## Edge Cases and Risks

1. **Vite create in non-empty directory**: `web/` has `.gitkeep`. The `npm create vite@latest . -- --template react-ts` should handle this, but if it errors, delete `.gitkeep` first.

2. **Tailwind CSS v4 breaking changes**: v4 uses `@import "tailwindcss"` (not `@tailwind` directives) and the `@tailwindcss/vite` plugin (not PostCSS config). Make sure NOT to use the old v3 setup.

3. **React Router v7**: Uses `react-router` package (not `react-router-dom`). The `createBrowserRouter` API is the recommended approach.

4. **Kratos proxy rewrite**: The `/.ory` prefix rewrite must strip the prefix correctly. Test by navigating to `/.ory/self-service/login/browser` in the browser — it should proxy to Kratos and return a flow JSON.

5. **Cookie handling**: `credentials: "include"` is required for cross-origin cookie sending, but in dev the proxy makes everything same-origin. In production, the SPA is served from the same origin as the API, so this also works. No CORS issues expected.

6. **Node version**: Dockerfile uses `node:22-alpine`. Ensure local development also uses Node 22+ for consistency.

## Test Strategy

This task is mostly scaffolding with no business logic, so there's no unit test requirement. Verification is:

1. **Build passes**: `npm run build` succeeds without errors
2. **Type check passes**: `npx tsc --noEmit` succeeds
3. **Dev server starts**: `npm run dev` starts on port 5173
4. **Routes work**: Navigating to `/`, `/auth/login`, `/auth/registration` renders the correct placeholder pages
5. **404 works**: Navigating to `/nonexistent` shows the NotFound page
6. **Proxy works** (manual, requires running services): `/api/v1/posts` proxies to Go server, `/.ory/self-service/login/browser` proxies to Kratos
