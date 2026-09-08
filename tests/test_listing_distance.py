from app.core.geo import haversine_km
from app.models.listing import Listing

# University of Lagos, Akoka — the reference point the seed data clusters around.
UNILAG = (6.5158, 3.3898)


def _listing(db_session, title, lat, lng):
    obj = Listing(
        title=title,
        type="Studio",
        price_per=800,
        total=800,
        deposit=800,
        walk=10,
        latitude=lat,
        longitude=lng,
        grad="from-coral to-sunshine",
        emoji="🏠",
    )
    db_session.add(obj)
    return obj


def test_haversine_known_distance():
    # Lagos (Marina) -> Abuja (city centre) is ~525 km as the crow flies.
    lagos = (6.4550, 3.3841)
    abuja = (9.0765, 7.3986)
    km = haversine_km(*lagos, *abuja)
    assert 500 < km < 545


def test_haversine_zero_distance():
    assert haversine_km(*UNILAG, *UNILAG) == 0


async def test_listings_sorted_nearest_first(client, db_session):
    _listing(db_session, "Far", 6.5400, 3.4100)
    _listing(db_session, "Near", 6.5170, 3.3910)
    _listing(db_session, "Middle", 6.5250, 3.3990)
    await db_session.commit()

    res = await client.get(f"/api/v1/listings?near_lat={UNILAG[0]}&near_lng={UNILAG[1]}")
    assert res.status_code == 200, res.text
    body = res.json()
    assert [item["title"] for item in body] == ["Near", "Middle", "Far"]
    # distance_km is populated and ascending
    distances = [item["distance_km"] for item in body]
    assert all(d is not None for d in distances)
    assert distances == sorted(distances)


async def test_radius_km_filters_out_distant_listings(client, db_session):
    _listing(db_session, "Inside", 6.5170, 3.3910)
    _listing(db_session, "Outside", 6.6500, 3.5000)
    await db_session.commit()

    res = await client.get(
        f"/api/v1/listings?near_lat={UNILAG[0]}&near_lng={UNILAG[1]}&radius_km=5"
    )
    assert res.status_code == 200, res.text
    titles = [item["title"] for item in res.json()]
    assert titles == ["Inside"]


async def test_listings_without_coordinates_excluded_from_proximity_search(client, db_session):
    _listing(db_session, "Has coords", 6.5170, 3.3910)
    no_coords = Listing(
        title="No coords",
        type="Studio",
        price_per=800,
        total=800,
        deposit=800,
        walk=10,
        grad="from-coral to-sunshine",
        emoji="🏠",
    )
    db_session.add(no_coords)
    await db_session.commit()

    # Proximity search drops the coordinate-less listing...
    res = await client.get(f"/api/v1/listings?near_lat={UNILAG[0]}&near_lng={UNILAG[1]}")
    assert [item["title"] for item in res.json()] == ["Has coords"]

    # ...but a plain search still returns both, with distance_km null.
    res = await client.get("/api/v1/listings")
    body = {item["title"]: item for item in res.json()}
    assert set(body) == {"Has coords", "No coords"}
    assert body["No coords"]["distance_km"] is None
    assert body["Has coords"]["distance_km"] is None
