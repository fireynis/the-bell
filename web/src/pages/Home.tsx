import { Link } from "react-router";
import { useAuth } from "../context/AuthContext.tsx";

export default function Home() {
  const { session, logout } = useAuth();

  return (
    <div className="mx-auto max-w-2xl p-4">
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-bold">Feed</h1>
        <div className="flex items-center gap-3">
          {session && (
            <span className="text-sm text-gray-600">
              {session.identity.traits.name ?? session.identity.traits.email}
            </span>
          )}
          <Link
            to="/auth/settings"
            className="text-sm text-indigo-600 hover:text-indigo-500"
          >
            Settings
          </Link>
          <button
            onClick={logout}
            className="rounded-md bg-gray-100 px-3 py-1 text-sm text-gray-700 hover:bg-gray-200"
          >
            Sign out
          </button>
        </div>
      </div>
      <p className="text-gray-500">No posts yet.</p>
    </div>
  );
}
