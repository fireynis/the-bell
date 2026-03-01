import type { RouteObject } from "react-router";
import RequireAuth from "./components/RequireAuth.tsx";
import RequireRole from "./components/RequireRole.tsx";
import Home from "./pages/Home";
import Compose from "./pages/Compose";
import Login from "./pages/auth/Login";
import Registration from "./pages/auth/Registration";
import Settings from "./pages/auth/Settings";
import Recovery from "./pages/auth/Recovery";
import Verification from "./pages/auth/Verification";
import NotFound from "./pages/NotFound";
import Queue from "./pages/moderation/Queue";
import UserHistory from "./pages/moderation/UserHistory";

export const routes: RouteObject[] = [
  // Protected routes
  {
    element: <RequireAuth />,
    children: [
      { path: "/", element: <Home /> },
      { path: "/compose", element: <Compose /> },
      { path: "/auth/settings", element: <Settings /> },
      // Moderation routes (moderator+ required)
      {
        element: <RequireRole minRole="moderator" />,
        children: [
          { path: "/moderation", element: <Queue /> },
          { path: "/moderation/users/:id", element: <UserHistory /> },
        ],
      },
    ],
  },
  // Public auth routes
  { path: "/auth/login", element: <Login /> },
  { path: "/auth/registration", element: <Registration /> },
  { path: "/auth/recovery", element: <Recovery /> },
  { path: "/auth/verification", element: <Verification /> },
  { path: "*", element: <NotFound /> },
];
