import type { RouteObject } from "react-router";
import RequireAuth from "./components/RequireAuth.tsx";
import Home from "./pages/Home";
import Compose from "./pages/Compose";
import Profile from "./pages/Profile";
import Login from "./pages/auth/Login";
import Registration from "./pages/auth/Registration";
import Settings from "./pages/auth/Settings";
import Recovery from "./pages/auth/Recovery";
import Verification from "./pages/auth/Verification";
import NotFound from "./pages/NotFound";

export const routes: RouteObject[] = [
  // Protected routes
  {
    element: <RequireAuth />,
    children: [
      { path: "/", element: <Home /> },
      { path: "/compose", element: <Compose /> },
      { path: "/profile", element: <Profile /> },
      { path: "/profile/:userId", element: <Profile /> },
      { path: "/auth/settings", element: <Settings /> },
    ],
  },
  // Public auth routes
  { path: "/auth/login", element: <Login /> },
  { path: "/auth/registration", element: <Registration /> },
  { path: "/auth/recovery", element: <Recovery /> },
  { path: "/auth/verification", element: <Verification /> },
  { path: "*", element: <NotFound /> },
];
