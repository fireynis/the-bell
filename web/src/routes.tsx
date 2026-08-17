import type { RouteObject } from "react-router";
import RequireAuth from "./components/RequireAuth.tsx";
import RequireRole from "./components/RequireRole.tsx";
import AppLayout from "./components/AppLayout.tsx";
import Home from "./pages/Home";
import Compose from "./pages/Compose";
import Neighbors from "./pages/Neighbors";
import Profile from "./pages/Profile";
import Admin from "./pages/Admin";
import Approvals from "./pages/admin/Approvals";
import Login from "./pages/auth/Login";
import Registration from "./pages/auth/Registration";
import Settings from "./pages/auth/Settings";
import Recovery from "./pages/auth/Recovery";
import Verification from "./pages/auth/Verification";
import Queue from "./pages/moderation/Queue";
import UserHistory from "./pages/moderation/UserHistory";
import NotFound from "./pages/NotFound";

export const routes: RouteObject[] = [
  {
    element: <RequireAuth />,
    children: [
      {
        element: <AppLayout />,
        children: [
          { path: "/", element: <Home /> },
          { path: "/compose", element: <Compose /> },
          // No RequireRole: the directory is open to every signed-in member,
          // pending included. Finding a neighbour who knows you is how a
          // pending member stops being one.
          { path: "/neighbors", element: <Neighbors /> },
          { path: "/profile", element: <Profile /> },
          { path: "/profile/:userId", element: <Profile /> },
          { path: "/auth/settings", element: <Settings /> },
          {
            element: <RequireRole minRole="moderator" />,
            children: [
              { path: "/moderation", element: <Queue /> },
              { path: "/moderation/users/:id", element: <UserHistory /> },
            ],
          },
        ],
      },
      {
        // Town Hall is a dashboard rather than a column of posts, so it gets
        // the wide measure. A second layout branch rather than a flag the
        // layout reads off the route: this keeps AppLayout free of any
        // knowledge of which paths exist.
        element: <AppLayout wide />,
        children: [
          {
            element: <RequireRole minRole="council" />,
            children: [
              { path: "/admin", element: <Admin /> },
              // The approval queue has a page of its own rather than a panel on
              // the dashboard: fifty applicants is a normal town launch, and
              // working through them is a different activity from reading where
              // the town stands. Same guard, same wide measure.
              { path: "/admin/approvals", element: <Approvals /> },
            ],
          },
        ],
      },
    ],
  },
  { path: "/auth/login", element: <Login /> },
  { path: "/auth/registration", element: <Registration /> },
  { path: "/auth/recovery", element: <Recovery /> },
  { path: "/auth/verification", element: <Verification /> },
  { path: "*", element: <NotFound /> },
];
