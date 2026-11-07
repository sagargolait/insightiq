import { NextResponse, type NextRequest } from "next/server";

const SESSION_COOKIE = "insightiq_session";

// Cheap presence check only. The API validates the session on every call;
// a stale cookie still ends at /login via the 401 handling in the client.
export function proxy(request: NextRequest) {
  const hasSession = request.cookies.has(SESSION_COOKIE);
  const { pathname, search } = request.nextUrl;

  if (!hasSession && pathname.startsWith("/orgs")) {
    const url = new URL("/login", request.url);
    url.searchParams.set("next", pathname + search);
    return NextResponse.redirect(url);
  }

  if (hasSession && (pathname === "/login" || pathname === "/register")) {
    return NextResponse.redirect(new URL("/orgs", request.url));
  }

  return NextResponse.next();
}

export const config = {
  matcher: ["/orgs/:path*", "/login", "/register"],
};
