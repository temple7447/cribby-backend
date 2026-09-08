"""Seed the DB with the same demo data the frontend used to hardcode in
lib/data.ts, so swapping to the real API doesn't change what's on screen.
Run with: uv run python scripts/seed.py
"""

import asyncio
import sys
from datetime import date
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from app.core.security import hash_password
from app.db.session import AsyncSessionLocal
from app.models.booking import Booking
from app.models.listing import Listing
from app.models.message import Conversation, Message
from app.models.place import Place
from app.models.roommate import RoommateProfile
from app.models.user import User

AMENITIES = [
    "🛏️ Furnished",
    "⚡ Utilities incl.",
    "🐱 Pet-friendly",
    "🧺 Laundry",
    "📶 Wi-Fi",
    "🅿️ Parking",
    "❄️ A/C",
    "🚲 Bike storage",
]

# Coordinates scattered around a campus reference point (~6.5158, 3.3898,
# the University of Lagos, Akoka) so proximity search / map display has real
# data to work with. Offsets roughly track the `walk` minutes.
LISTINGS = [
    dict(
        title="Sunny Studio on Maple St",
        type="Studio",
        price_per=890,
        total=890,
        walk=8,
        latitude=6.5194,
        longitude=3.3934,
        rating=4.8,
        reviews=32,
        verified=True,
        hot=True,
        grad="from-coral to-sunshine",
        emoji="🌇",
    ),
    dict(
        title="The Foundry House",
        type="Shared 4BR",
        price_per=480,
        total=1920,
        walk=12,
        latitude=6.5099,
        longitude=3.3951,
        rating=4.6,
        reviews=58,
        verified=True,
        video=True,
        grad="from-violet to-coral",
        emoji="🏘️",
    ),
    dict(
        title="Corner 1BR with City View",
        type="1BR",
        price_per=1150,
        total=1150,
        walk=15,
        latitude=6.5241,
        longitude=3.3831,
        rating=4.4,
        reviews=19,
        grad="from-mint to-violet",
        emoji="🏙️",
    ),
    dict(
        title="Cozy 2BR + Balcony",
        type="2BR",
        price_per=620,
        total=1240,
        walk=10,
        latitude=6.5112,
        longitude=3.3849,
        rating=4.9,
        reviews=41,
        hot=True,
        grad="from-sunshine to-mint",
        emoji="🪴",
    ),
    dict(
        title="Garden Studio",
        type="Studio",
        price_per=760,
        total=760,
        walk=20,
        latitude=6.5261,
        longitude=3.4001,
        rating=4.2,
        reviews=12,
        video=True,
        grad="from-mint to-sunshine",
        emoji="🌿",
    ),
    dict(
        title="Loft near North Gate",
        type="Shared 3BR",
        price_per=540,
        total=1620,
        walk=6,
        latitude=6.5131,
        longitude=3.3914,
        rating=4.7,
        reviews=27,
        verified=True,
        grad="from-coral to-violet",
        emoji="🛋️",
    ),
]

ROOMMATES = [
    dict(
        name="Zoe",
        age=20,
        school="State University",
        year="Junior",
        budget_min=450,
        budget_max=600,
        move_in_date="Aug 1",
        tags=["🦉 Night owl", "🧼 Tidy", "🐱 Cat ok"],
        verified=True,
        grad="from-coral to-sunshine",
        emoji="🎨",
        bio="Design major, plant mom, professional overthinker. Labels their leftovers.",
        looking_for="Shared 2-4BR, chill vibe, close to campus.",
    ),
    dict(
        name="Marcus",
        age=21,
        school="State University",
        year="Senior",
        budget_min=500,
        budget_max=700,
        move_in_date="Aug 15",
        tags=["🐦 Early bird", "🏋️ Gym rat", "🎧 Chill"],
        grad="from-violet to-mint",
        emoji="🏀",
        bio="Engineering senior. Up at 6, quiet by 10. Will fix your Wi-Fi.",
        looking_for="Quiet 1-2BR near the gym.",
    ),
    dict(
        name="Priya",
        age=19,
        school="State University",
        year="Sophomore",
        budget_min=400,
        budget_max=550,
        move_in_date="Sep 1",
        tags=["📚 Study vibe", "🌱 Vegan", "🧼 Tidy"],
        verified=True,
        grad="from-mint to-sunshine",
        emoji="📖",
        bio="Pre-med, chai enthusiast. Looking for a calm home base near the library.",
        looking_for="Studio or shared 2BR, near library.",
    ),
    dict(
        name="Tom",
        age=22,
        school="State University",
        year="Grad",
        budget_min=600,
        budget_max=800,
        move_in_date="Aug 1",
        tags=["🦉 Night owl", "🎮 Gamer", "🐕 Dog ok"],
        grad="from-sunshine to-coral",
        emoji="🎸",
        bio="Grad student, part-time barista. I come with a espresso machine and strong opinions.",
        looking_for="Shared house, pet-friendly.",
    ),
]

# Points of interest around the same campus (~6.5158, 3.3898) students search
# for — "where's the nearest pharmacy / ATM / bus stop from this listing".
PLACES = [
    dict(name="Akoka General Hospital", category="hospital", emoji="🏥",
         address="12 Akoka Rd", latitude=6.5185, longitude=3.3902),
    dict(name="Bariga Health Centre", category="clinic", emoji="🩺",
         address="Bariga", latitude=6.5350, longitude=3.3830),
    dict(name="MedPlus Pharmacy Yaba", category="pharmacy", emoji="💊",
         address="Herbert Macaulay Way, Yaba", latitude=6.5145, longitude=3.3875),
    dict(name="HealthPlus Pharmacy Akoka", category="pharmacy", emoji="💊",
         address="St. Finbarr's Rd, Akoka", latitude=6.5205, longitude=3.3948),
    dict(name="GTBank Akoka", category="bank", emoji="🏦",
         address="University Rd, Akoka", latitude=6.5169, longitude=3.3925),
    dict(name="Access Bank Yaba", category="bank", emoji="🏦",
         address="Commercial Ave, Yaba", latitude=6.5098, longitude=3.3790),
    dict(name="UBA ATM — Unilag Gate", category="atm", emoji="🏧",
         address="University Main Gate", latitude=6.5162, longitude=3.3961),
    dict(name="Yaba Market", category="market", emoji="🛒",
         address="Yaba", latitude=6.5090, longitude=3.3775),
    dict(name="Akoka Foodstuff Market", category="market", emoji="🛒",
         address="Akoka", latitude=6.5220, longitude=3.3890),
    dict(name="Sweet Sensation Akoka", category="restaurant", emoji="🍽️",
         address="University Rd", latitude=6.5178, longitude=3.3930),
    dict(name="Iya Basira Buka", category="restaurant", emoji="🍲",
         address="Akoka", latitude=6.5195, longitude=3.3908),
    dict(name="Unilag Main Gate Bus Stop", category="bus_stop", emoji="🚌",
         address="University Rd", latitude=6.5155, longitude=3.3965),
    dict(name="Bariga Bus Stop", category="bus_stop", emoji="🚌",
         address="Bariga Rd", latitude=6.5360, longitude=3.3845),
    dict(name="FitZone Gym Akoka", category="gym", emoji="🏋️",
         address="St. Finbarr's Rd", latitude=6.5210, longitude=3.3875),
    dict(name="CleanPro Laundry", category="laundry", emoji="🧺",
         address="Akoka", latitude=6.5188, longitude=3.3945),
    dict(name="Akoka Police Station", category="police", emoji="🚓",
         address="Akoka", latitude=6.5240, longitude=3.3860),
    dict(name="Chapel of the Healing Cross", category="worship", emoji="⛪",
         address="Unilag campus", latitude=6.5170, longitude=3.3985),
    dict(name="Unilag Central Mosque", category="worship", emoji="🕌",
         address="Unilag campus", latitude=6.5175, longitude=3.3975),
]


async def seed() -> None:
    async with AsyncSessionLocal() as db:
        maya = User(
            email="maya@stateuniversity.edu",
            hashed_password=hash_password("password123"),
            name="Maya Chen",
            school="State University",
            avatar_emoji="🎓",
            verification_status="verified",
        )
        db.add(maya)

        listings = [
            Listing(deposit=item["price_per"], amenities=AMENITIES, **item) for item in LISTINGS
        ]
        db.add_all(listings)

        db.add_all([Place(**p) for p in PLACES])

        roommate_users = []
        for rm in ROOMMATES:
            u = User(
                email=f"{rm['name'].lower()}@stateuniversity.edu",
                hashed_password=hash_password("password123"),
                name=rm["name"],
                school=rm["school"],
                verification_status="verified" if rm.get("verified") else "unverified",
            )
            db.add(u)
            roommate_users.append(u)

        await db.flush()  # assign ids to maya, listings, roommate_users

        for u, rm in zip(roommate_users, ROOMMATES):
            # "verified" isn't a settable field anymore — RoommateProfile.verified is now a
            # property derived from the owning user's verification_status (set above).
            profile_fields = {k: v for k, v in rm.items() if k != "verified"}
            db.add(RoommateProfile(user_id=u.id, **profile_fields))

        sunny_studio, foundry_house = listings[0], listings[1]

        booking1 = Booking(
            user_id=maya.id,
            listing_id=sunny_studio.id,
            move_in_date=date(2026, 8, 1),
            lease_months=12,
            status="pending",
            request_sent=True,
            landlord_approved=False,
        )
        booking2 = Booking(
            user_id=maya.id,
            listing_id=foundry_house.id,
            move_in_date=date(2026, 8, 1),
            lease_months=12,
            status="approved",
            request_sent=True,
            landlord_approved=True,
            guarantor_status="signed",
            guarantor_signed=True,
        )
        db.add_all([booking1, booking2])

        landlord_convo = Conversation(
            user_id=maya.id,
            kind="landlord",
            listing_id=sunny_studio.id,
            title="Sandra",
            subtitle="Sunny Studio on Maple St",
            emoji="👩‍💼",
            grad="from-coral to-sunshine",
        )
        roommate_convo = Conversation(
            user_id=maya.id,
            kind="roommate",
            roommate_profile_id=None,
            title="Zoe",
            subtitle="Design major · Junior",
            emoji="🎨",
            grad="from-coral to-sunshine",
        )
        db.add_all([landlord_convo, roommate_convo])
        await db.flush()

        db.add_all(
            [
                Message(
                    conversation_id=landlord_convo.id,
                    sender="system",
                    text="Booking request sent ✓",
                ),
                Message(
                    conversation_id=roommate_convo.id,
                    sender="them",
                    text="Hey! Are you still looking for a place near North Gate?",
                ),
            ]
        )

        await db.commit()
        print(
            f"Seeded: 1 primary user (maya@stateuniversity.edu / password123), "
            f"{len(listings)} listings, {len(PLACES)} places, "
            f"{len(ROOMMATES)} roommate profiles, 2 bookings, 2 conversations."
        )


if __name__ == "__main__":
    asyncio.run(seed())
