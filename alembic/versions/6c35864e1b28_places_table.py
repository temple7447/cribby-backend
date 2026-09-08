"""places table

Revision ID: 6c35864e1b28
Revises: b2c3d4e5f6a7
Create Date: 2026-09-08 14:10:40.069957

"""
from typing import Sequence, Union

from alembic import op
import sqlalchemy as sa


# revision identifiers, used by Alembic.
revision: str = '6c35864e1b28'
down_revision: Union[str, Sequence[str], None] = 'b2c3d4e5f6a7'
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    """Upgrade schema."""
    op.create_table(
        'places',
        sa.Column('id', sa.Integer(), nullable=False),
        sa.Column('name', sa.String(length=200), nullable=False),
        sa.Column('category', sa.String(length=50), nullable=False),
        sa.Column('address', sa.String(length=300), nullable=True),
        sa.Column('latitude', sa.Float(), nullable=False),
        sa.Column('longitude', sa.Float(), nullable=False),
        sa.Column('emoji', sa.String(length=8), nullable=False),
        sa.Column(
            'created_at',
            sa.DateTime(timezone=True),
            server_default=sa.text('(CURRENT_TIMESTAMP)'),
            nullable=False,
        ),
        sa.PrimaryKeyConstraint('id'),
    )
    with op.batch_alter_table('places', schema=None) as batch_op:
        batch_op.create_index(batch_op.f('ix_places_category'), ['category'], unique=False)


def downgrade() -> None:
    """Downgrade schema."""
    with op.batch_alter_table('places', schema=None) as batch_op:
        batch_op.drop_index(batch_op.f('ix_places_category'))
    op.drop_table('places')
