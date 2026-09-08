"""Geo helpers shared by proximity search across resources (listings, places)."""

import math

# Mean Earth radius (km), IUGG value — good enough for "how close is this".
EARTH_RADIUS_KM = 6371.0088


def haversine_km(lat1: float, lon1: float, lat2: float, lon2: float) -> float:
    """Great-circle distance between two lat/lng points, in kilometres.

    Straight-line ("as the crow flies") distance — not walking/driving distance.
    Enough to sort results by nearness and filter a radius without any map API.
    """
    phi1, phi2 = math.radians(lat1), math.radians(lat2)
    d_phi = math.radians(lat2 - lat1)
    d_lambda = math.radians(lon2 - lon1)
    a = math.sin(d_phi / 2) ** 2 + math.cos(phi1) * math.cos(phi2) * math.sin(d_lambda / 2) ** 2
    return 2 * EARTH_RADIUS_KM * math.asin(math.sqrt(a))


def rank_by_distance(
    rows: list,
    origin_lat: float,
    origin_lng: float,
    *,
    radius_km: float | None = None,
) -> list:
    """Stamp each row with `.distance_km` from the origin, drop rows with no
    coordinates (or outside radius_km, if given), and return them nearest-first.

    Mutates the rows in place — they're expected to be ORM instances whose
    matching Pydantic schema exposes an optional `distance_km` field. Done in
    Python because SQLite has no trig functions and these tables are small.
    """
    ranked = []
    for row in rows:
        if row.latitude is None or row.longitude is None:
            continue
        row.distance_km = round(
            haversine_km(origin_lat, origin_lng, row.latitude, row.longitude), 2
        )
        if radius_km is not None and row.distance_km > radius_km:
            continue
        ranked.append(row)
    ranked.sort(key=lambda r: r.distance_km)
    return ranked
